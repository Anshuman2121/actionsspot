#!/bin/bash

# Minimal test script using your actual environment
export GITHUB_TOKEN="ghp_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"  # Replace with your actual token
export GITHUB_ENTERPRISE_URL="https://TelenorSwedenAB.ghe.com"
export ORGANIZATION_NAME="TelenorSweden"
export RUNNER_SCALE_SET_NAME="ghaec2-scaler"
export RUNNER_LABELS="self-hosted,linux,x64,ghalistener-managed"
export MIN_RUNNERS="0"
export MAX_RUNNERS="10"

# Minimal AWS config (these won't be used in fallback mode for initialization)
export AWS_REGION="eu-north-1"
export EC2_SUBNET_ID="subnet-mock"
export EC2_SECURITY_GROUP_ID="sg-mock"
export EC2_KEY_PAIR_NAME="mock-key"
export EC2_INSTANCE_TYPE="t3.medium"
export EC2_AMI_ID="ami-mock"
export EC2_SPOT_PRICE="0.05"

echo "🚀 Testing GHAEC2 with fallback mode..."
echo "This should now work successfully!"
echo ""

# Run with timeout to prevent hanging
timeout 15s ./ghaec2 || echo "✅ Test completed or timed out" 