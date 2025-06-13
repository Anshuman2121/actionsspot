#!/bin/bash

echo "=== Using Existing Scale Set 'arc-runner-set' ==="
echo ""
echo "SOLUTION: Since your token doesn't have permissions to create new scale sets,"
echo "          we'll use the existing 'arc-runner-set' (ID: 1) that's already available."
echo ""

# GitHub Configuration
export GITHUB_TOKEN="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"
export GITHUB_ENTERPRISE_URL="https://TelenorSwedenAB.ghe.com"
export ORGANIZATION_NAME="TelenorSweden"

# Use existing scale set
export RUNNER_SCALE_SET_NAME="arc-runner-set"  # Use existing scale set
export RUNNER_SCALE_SET_ID="1"                 # Specify the known ID
export RUNNER_GROUP_ID="1"                     # Default group (as seen in logs)
export RUNNER_LABELS="arc-runner-set"          # Use the existing label

# AWS Configuration (use your values)
export MIN_RUNNERS="0"
export MAX_RUNNERS="10"
export AWS_REGION="eu-north-1"
export EC2_SUBNET_ID="subnet-12345"            # Replace with your subnet
export EC2_SECURITY_GROUP_ID="sg-12345"        # Replace with your security group
export EC2_KEY_PAIR_NAME="your-key-pair"       # Replace with your key pair
export EC2_INSTANCE_TYPE="t3.medium"
export EC2_AMI_ID="ami-12345"                  # Replace with your AMI
export EC2_SPOT_PRICE="0.05"

echo "Configuration:"
echo "  Scale Set: $RUNNER_SCALE_SET_NAME (ID: $RUNNER_SCALE_SET_ID)"
echo "  Runner Group: $RUNNER_GROUP_ID"
echo "  Labels: $RUNNER_LABELS"
echo ""

echo "IMPORTANT: You'll need to update your workflow to use:"
echo "  runs-on: [arc-runner-set]"
echo ""
echo "Instead of:"
echo "  runs-on: [self-hosted, linux, x64, ghalistener-managed]"
echo ""

read -p "Press Enter to start the scaler, or Ctrl+C to cancel..."

echo "Starting ghaec2 with existing scale set..."
./ghaec2 