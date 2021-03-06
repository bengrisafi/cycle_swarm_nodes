package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/client"
)

type dockerConstuct struct {
	context context.Context
	client  client.Client
}

type dockerNode struct {
	awsId       string
	awsHostname string
	dkrNodeId   string
	dkrHostname string
	dkrNodeType string
}

func main() {
	ip := os.Args[1]
	profile := os.Args[2]
	if profile == "" {
		profile = "default"
	}
	d := connectToDocker()
	s := createAWSSession(profile)
	count := getCurrentNodeCount(d)

	fmt.Printf("Working on %s\n", ip)
	fmt.Printf("there are %d nodes\n", count)

	node := drainDockerNode(d, ip)
	shutdownAWSMachine(node, profile, s)
	removeDockerNode(node, d)
	confirmNewNode(d, count)
}

func connectToDocker() dockerConstuct {

	ctx := context.Background()
	cli, err := client.NewEnvClient()

	if err != nil {
		panic(err)
	}

	d := dockerConstuct{
		context: ctx,
		client:  *cli,
	}
	return d
}

func createAWSSession(profile string) session.Session {

	creds := credentials.NewSharedCredentials("/mnt/c/Users/ben.grisafi/.aws/credentials", profile)
	config := aws.NewConfig()
	config.Credentials = creds

	switch profile {
	case "Prod", "prodwest":
		config.Region = aws.String("us-west-2")
	case "ProdEU", "prodeu":
		config.Region = aws.String("eu-central-1")
	case "", "NonProd":
		config.Region = aws.String("us-east-1")
	default:
		fmt.Printf("Unknown profile %s \nValid profiles are \nProd, ProdEU, NonProd\n", profile)
		panic("bad profile")
	}
	fmt.Println(profile)
	sess := session.Must(session.NewSessionWithOptions(session.Options{
		Profile: profile,
		Config:  *config,
	}))

	return *sess
}

func getCurrentNodeCount(d dockerConstuct) int {

	nodes, nerr := d.client.NodeList(d.context, types.NodeListOptions{})
	if nerr != nil {
		panic(nerr)
	}
	count := 0
	for _, n := range nodes {
		if n.Status.State == "ready" {
			count++
		}
	}
	return len(nodes)
}

func buildNodeStruct(d dockerConstuct) {}

func drainDockerNode(dockerConnection dockerConstuct, ip string) swarm.Node {

	ctx := dockerConnection.context
	cli := dockerConnection.client

	host := strings.Replace(ip, ".", "-", -1)
	hostname := "name=ip-" + host

	fmt.Printf("Hostname:%s\n", hostname)
	filterArgs := filters.NewArgs()
	filters.ParseFlag(hostname, filterArgs)
	options := types.NodeListOptions{Filters: filterArgs}

	nodes, lerr := cli.NodeList(ctx, options)
	if len(nodes) == 0 {
		fmt.Printf("There were no nodes \n")
		os.Exit(4)
	}

	if lerr != nil {
		fmt.Println("Problem with the list filter")
		panic(lerr)
	}

	// node availability states = active, pause, drain
	nodes[0].Spec.Availability = "drain"

	fmt.Printf("updating node: %s \n", nodes[0].Description.Hostname)
	r := cli.NodeUpdate(ctx, nodes[0].ID, nodes[0].Version, nodes[0].Spec)
	if r != nil {
		fmt.Printf("err:%s\n", r)
		panic(r)
	}

	// confirm node is drained
	time.Sleep(3 * time.Second)
	wait := 1
	//wait total of 30s for node to drain containers
	for wait < 6 {
		wait += wait
		status := checkContainerCount(dockerConnection)
		if status == 0 {
			break
		}
		time.Sleep(5 * time.Second)
	}
	fmt.Printf("node drained %s\n", nodes[0].Description.Hostname)
	bob := nodes[0]
	return bob
}

func checkContainerCount(d dockerConstuct) int {
	cli := d.client
	ctx := d.context
	c, listerr := cli.ContainerList(ctx, types.ContainerListOptions{})
	if listerr != nil {
		panic(listerr)
	}

	return len(c)
}

func waitForUpdate(id string, d dockerConstuct) bool {

	node, n, err := d.client.NodeInspectWithRaw(d.context, id)
	if err != nil {
		panic(err)
	}
	fmt.Println(string(n[:]))
	if node.Spec.Role == "worker" {
		return true
	}
	return false
}

func removeDockerNode(r swarm.Node, dockerConnection dockerConstuct) {
	ctx := dockerConnection.context
	cli := dockerConnection.client

	fmt.Printf("index=%d", r.Version.Index)
	dockerType := r.Spec.Role
	confirmNodeDown(r, dockerConnection)
	// if manager we need to demote
	if dockerType == "manager" {
		fmt.Printf("node is a manager. demoting:%s\n", r.ID)
		r.Spec.Role = "worker"
		b, by, gerr := cli.NodeInspectWithRaw(ctx, r.ID)
		fmt.Println(string(by[:]))
		if gerr != nil {
			panic(gerr)
		}
		b.Spec.Role = "worker"
		err := cli.NodeUpdate(ctx, b.ID, b.Version, b.Spec)
		if err != nil {
			fmt.Println("problem with the update")
			panic(err)
		}
		for waitForUpdate(b.ID, dockerConnection) == false {
			time.Sleep(10 * time.Second)
		}
		fmt.Printf("node demoted\nremoving node:%s\n", b.ID)
		n, nby, gerr := cli.NodeInspectWithRaw(ctx, b.ID)
		fmt.Println(string(nby[:]))
		if gerr != nil {
			fmt.Println("problem with the removal")
			panic(gerr)
		}
		options := types.NodeRemoveOptions{}
		options.Force = true
		erm := cli.NodeRemove(ctx, n.ID, options)
		if erm != nil {
			panic(erm)
		} else {
			fmt.Printf("removed %s\n", r.Description.Hostname)
		}
		fmt.Printf("node removed\n")
	} else {
		fmt.Printf("node is a worker\nremoving:%s\n", r.ID)
		erm := cli.NodeRemove(ctx, r.ID, types.NodeRemoveOptions{})
		if erm != nil {
			panic(erm)
		}
		fmt.Printf("node removed\n")
	}
}
func confirmNodeDown(b swarm.Node, d dockerConstuct) swarm.Node {

	arg := "ID=" + b.ID
	n := b
	time.Sleep(10 * time.Second)
	if b.Status.State == "ready" {
		wait := 1
		//wait 120s then break out
		for wait < 12 {
			wait += wait
			n := checkNodeStatus(arg, d)
			if strings.ToLower(string(n.Status.State)) == "down" {
				return n
			}
			time.Sleep(10 * time.Second)
		}
	}
	fmt.Printf("Waited 120s for %s to shutdown.\n", b.ID)
	os.Exit(3)
	return n
}

func checkNodeStatus(arg string, d dockerConstuct) swarm.Node {
	ctx := d.context
	cli := d.client

	filterArgs := filters.NewArgs()
	filters.ParseFlag(arg, filterArgs)
	options := types.NodeListOptions{Filters: filterArgs}
	nodes, lerr := cli.NodeList(ctx, options)

	if lerr != nil {
		fmt.Println("Problem with the list filter")

	}

	return nodes[0]
}

func shutdownAWSMachine(b swarm.Node, profile string, session session.Session) {

	svcEC2 := ec2.New(&session)
	hostname := b.Description.Hostname + ".ec2.internal"
	if profile != "default" {
		hostname = b.Description.Hostname + "." + *session.Config.Region + ".compute.internal"
	}
	fmt.Println(hostname)
	sinput := &ec2.DescribeInstancesInput{
		Filters: []*ec2.Filter{
			&ec2.Filter{
				Name:   aws.String("private-dns-name"),
				Values: []*string{aws.String(hostname)},
			},
		},
	}
	results, err := svcEC2.DescribeInstances(sinput)
	if err != nil {
		fmt.Println("somethign is wrong with the query")
		panic(err)
	}

	if len(results.Reservations) == 0 {
		fmt.Println("no results found")
		panic(err)
	}
	svc := ec2.New(&session)
	input := &ec2.TerminateInstancesInput{
		InstanceIds: []*string{
			aws.String(*results.Reservations[0].Instances[0].InstanceId),
		},
		DryRun: aws.Bool(true),
	}

	result, err := svc.TerminateInstances(input)
	awsErr, ok := err.(awserr.Error)
	if ok && awsErr.Code() == "DryRunOperation" {
		// change to false to have this actually work
		input.DryRun = aws.Bool(false)
		result, err = svc.TerminateInstances(input)
		if err != nil {
			fmt.Println("Error", err)
		} else {
			fmt.Println("Success", result.TerminatingInstances)
		}
	} else {
		fmt.Println("Error", err)
	}
	// } else {
	// 	fmt.Printf("Instance not confirmed for removal in AWS, but is still drained in swarm %s\n", *results.Reservations[0].Instances[0].PrivateIpAddress)
	// 	os.Exit(1)
	// }
}

func askForConfirmation(s string) bool {
	reader := bufio.NewReader(os.Stdin)

	for {
		fmt.Printf("%s [y/n]: ", s)

		response, err := reader.ReadString('\n')
		if err != nil {
			log.Fatal(err)
		}

		response = strings.ToLower(strings.TrimSpace(response))

		if response == "y" || response == "yes" {
			return true
		} else if response == "n" || response == "no" {
			return false
		}
	}
}

func confirmNewNode(d dockerConstuct, count int) {

	n := getCurrentNodeCount(d)
	fmt.Printf("nodes %d/%d\n", n, count)
	final := "bad"
	if n != count {
		wait := 1
		// wait 15min for new aws node
		for wait < 30 {
			wait++
			n := getCurrentNodeCount(d)
			fmt.Printf("nodes %d/%d:%d\n", n, count, wait)
			time.Sleep(30 * time.Second)
			if n == count {
				fmt.Printf("Replacement node is joined to the swarm.\n")
				final = "good"
				break
			}

		}
		if final == "bad" {
			fmt.Printf("There was a problem with adding the new node. We waited 8m and no new node has been added.\nInvestigate\n")
			e := os.ErrInvalid
			os.Exit(13)
			panic(e)
		} else {
			fmt.Printf("Node cycle successful\n")
		}
	}
}
