package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// this test requires there be environment variables and credentials file setup
// TODO: should probably mock this out
func TestAwsSessionRegionSwitch(t *testing.T) {

	profiles := [5]string{"NonProd", "Prod", "ProdEU", "Foobar", ""}

	for _, profile := range profiles {
		switch profile {
		case "Prod":
			session := createAWSSession(profile)
			if *session.Config.Region != "us-west-2" {
				t.Error("expected", "us-west-2", "got", session.Config.Region)
			}
		case "ProdEU":
			session := createAWSSession(profile)
			if *session.Config.Region != "eu-central-1" {
				t.Error("expected", "eu-central-1", "got", session.Config.Region)
			}
		case "NonProd", "":
			session := createAWSSession(profile)
			if *session.Config.Region != "us-east-1" {
				t.Error("expected", "us-east-1", "got", session.Config.Region)
			}
		case "Foobar":
			assert.Panics(t, func() { createAWSSession(profile) }, "bad profile")
		}
	}
}

func testDockerNodeCount(t *testing.T) {

}
