#!/bin/bash

# Test script for GHAEC2
# Set your actual values here

export GITHUB_TOKEN="ghp_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"  # Replace with your actual token
export GITHUB_ENTERPRISE_URL="https://TelenorSwedenAB.ghe.com"
export ORGANIZATION_NAME="TelenorSweden"
export RUNNER_SCALE_SET_NAME="ghaec2-scaler"
export RUNNER_LABELS="self-hosted,linux,x64,ghalistener-managed"
export MIN_RUNNERS="0"
export MAX_RUNNERS="10"

# AWS Configuration (use your actual values)
export AWS_REGION="eu-north-1"
export EC2_SUBNET_ID="subnet-xxxxxxxxx"  # Replace with your subnet ID
export EC2_SECURITY_GROUP_ID="sg-xxxxxxxxx"  # Replace with your security group ID
export EC2_KEY_PAIR_NAME="your-key-pair"  # Replace with your key pair name
export EC2_INSTANCE_TYPE="t3.medium"
export EC2_AMI_ID="ami-xxxxxxxxx"  # Replace with your AMI ID
export EC2_SPOT_PRICE="0.05"

echo "Starting GHAEC2 with environment variables..."
echo "Organization: $ORGANIZATION_NAME"
echo "GitHub URL: $GITHUB_ENTERPRISE_URL"
echo "Scale Set: $RUNNER_SCALE_SET_NAME"
echo ""

./ghaec2 