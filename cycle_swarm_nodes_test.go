package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"testing"
)

func TestAwsEnvironmentConfigUsed(t *testing.T) {
	os.Create("~/.aws/testconfig")
	d1 := []byte("[foobar]\nregion=us-west-2\noutput=json")
	err := ioutil.WriteFile("~/.aws/testconfig", d1, 0644)
	profile := "foobar"
	session := createAWSSession(profile)
	actualSession := session.Session.Config.Region
	expectedSession := "us-west-2"
	if actualSession != expectedSession {
		fmt.Errorf("config file is not being sourced correctly")
	}
	check(err)
	if err != nil {
		fmt.Errorf("unable to write config file")
	}

}
