package main

import (
	"context"
	"log"
	"os"
	"time"
)

// Test function to demonstrate pipeline monitoring
func TestPipelineMonitoring() {
	log.Printf("🧪 Testing Pipeline Monitoring")

	// Load test configuration
	config := Config{
		GitHubToken:              os.Getenv("GITHUB_TOKEN"),
		GitHubEnterpriseURL:      "https://TelenorSwedenAB.ghe.com",
		OrganizationName:         "TelenorSweden",
		MinRunners:               0,
		MaxRunners:               5,
		EC2InstanceType:          "t3.medium",
		EC2AMI:                   "ami-0c02fb55956c7d316", // Ubuntu 20.04 LTS
		EC2SubnetID:              os.Getenv("EC2_SUBNET_ID"),
		EC2SecurityGroupID:       os.Getenv("EC2_SECURITY_GROUP_ID"),
		EC2KeyPairName:           os.Getenv("EC2_KEY_PAIR_NAME"),
		EC2SpotPrice:             "0.05",
		DynamoDBTableName:        "github-runners-test",
		RunnerLabels:             []string{"self-hosted", "linux", "x64", "lambda-managed"},
		CleanupOfflineRunners:    true,
	}

	ctx := context.Background()

	// Initialize clients
	gheClient := NewGHEClient(config)
	
	// Test GitHub Enterprise connectivity
	log.Printf("🔗 Testing GHE connectivity...")
	runners, err := gheClient.GetSelfHostedRunners(ctx)
	if err != nil {
		log.Printf("❌ Failed to connect to GHE: %v", err)
		return
	}
	log.Printf("✅ Connected to GHE. Found %d runners", len(runners.Runners))

	// Test pipeline checking
	log.Printf("📋 Checking for queued pipelines...")
	queuedRuns, err := gheClient.GetQueuedWorkflowRuns(ctx)
	if err != nil {
		log.Printf("❌ Failed to get queued workflows: %v", err)
		return
	}
	log.Printf("✅ Found %d queued workflows", len(queuedRuns.WorkflowRuns))

	// Display detailed information
	if len(queuedRuns.WorkflowRuns) > 0 {
		log.Printf("📝 Queued Workflows:")
		for i, run := range queuedRuns.WorkflowRuns {
			if i >= 3 {
				log.Printf("   ... and %d more", len(queuedRuns.WorkflowRuns)-3)
				break
			}
			log.Printf("   - ID: %d, Status: %s, Branch: %s", run.ID, run.Status, run.HeadBranch)
		}
	}

	// Display runner information
	if len(runners.Runners) > 0 {
		log.Printf("🤖 Current Runners:")
		for i, runner := range runners.Runners {
			if i >= 5 {
				log.Printf("   ... and %d more", len(runners.Runners)-5)
				break
			}
			status := "🟢"
			if runner.Status == "offline" {
				status = "🔴"
			}
			busy := ""
			if runner.Busy {
				busy = " (BUSY)"
			}
			log.Printf("   %s %s - %s%s", status, runner.Name, runner.Status, busy)
		}
	}

	// Simulate what the monitor would do
	log.Printf("🎯 Simulation: What would the monitor do?")
	
	availableRunners := 0
	busyRunners := 0
	for _, runner := range runners.Runners {
		if runner.Status == "online" {
			if runner.Busy {
				busyRunners++
			} else {
				availableRunners++
			}
		}
	}

	queuedCount := len(queuedRuns.WorkflowRuns)
	
	if queuedCount > 0 && availableRunners == 0 {
		log.Printf("📈 Would create %d new runners (queued: %d, available: %d)", 
			queuedCount, queuedCount, availableRunners)
	} else if queuedCount > availableRunners {
		log.Printf("📈 Would create %d new runners (queued: %d, available: %d)", 
			queuedCount-availableRunners, queuedCount, availableRunners)
	} else {
		log.Printf("✅ No new runners needed (queued: %d, available: %d)", 
			queuedCount, availableRunners)
	}

	log.Printf("🏁 Test completed")
}

// Main function for testing
func main() {
	if len(os.Args) > 1 && os.Args[1] == "test" {
		TestPipelineMonitoring()
	} else {
		log.Printf("Usage: go run . test")
	}
} 