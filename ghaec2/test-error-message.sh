#!/bin/bash

echo "🔧 Testing Improved Error Message Handling"
echo "==========================================="
echo ""
echo "Before: 'failed to decode response: invalid character '<' looking for beginning of value'"
echo "After:  Clear, user-friendly messages explaining the actual issue"
echo ""

# Set environment with valid token format but dummy content
export GITHUB_TOKEN="ghp_1234567890abcdef1234567890abcdef12345678"  # Valid format, dummy content
export GITHUB_ENTERPRISE_URL="https://TelenorSwedenAB.ghe.com"
export ORGANIZATION_NAME="TelenorSweden"
export RUNNER_SCALE_SET_NAME="test"
export AWS_REGION="eu-north-1"
export EC2_SUBNET_ID="subnet-test"
export EC2_SECURITY_GROUP_ID="sg-test"
export EC2_KEY_PAIR_NAME="test-key"
export EC2_INSTANCE_TYPE="t3.medium"
export EC2_AMI_ID="ami-test"
export EC2_SPOT_PRICE="0.05"

echo "Running test..."
echo ""

# Run the application and capture the specific error message
./ghaec2 2>&1 | grep -A1 -B1 "Actions Service\|admin connection\|fallback" | head -10 