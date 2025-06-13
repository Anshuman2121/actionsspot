#!/bin/bash

echo "=== Testing with Runner Group ID 6 ==="
echo ""

# Set environment variables with correct runner group ID
export GITHUB_TOKEN="$GITHUB_TOKEN"
export GITHUB_ENTERPRISE_URL="https://TelenorSwedenAB.ghe.com"
export ORGANIZATION_NAME="TelenorSweden"
export RUNNER_SCALE_SET_NAME="ghaec2-scaler"
export RUNNER_GROUP_ID="6"  # Set the correct runner group ID
export RUNNER_LABELS="self-hosted,linux,x64,ghalistener-managed"
export MIN_RUNNERS="0"
export MAX_RUNNERS="10"
export AWS_REGION="eu-north-1"
export EC2_SUBNET_ID="$EC2_SUBNET_ID"
export EC2_SECURITY_GROUP_ID="$EC2_SECURITY_GROUP_ID"
export EC2_KEY_PAIR_NAME="$EC2_KEY_PAIR_NAME"
export EC2_INSTANCE_TYPE="t3.medium"
export EC2_AMI_ID="$EC2_AMI_ID"
export EC2_SPOT_PRICE="0.05"

echo "Configuration:"
echo "  Runner Group ID: $RUNNER_GROUP_ID"
echo "  Scale Set Name: $RUNNER_SCALE_SET_NAME"
echo "  Labels: $RUNNER_LABELS"
echo ""

echo "Running ghaec2 with runner group ID 6..."
./ghaec2 