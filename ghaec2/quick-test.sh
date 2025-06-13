#!/bin/bash

echo "=== Quick Test with Existing Scale Set ==="
echo ""

# Your provided configuration
export GITHUB_TOKEN="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"
export GITHUB_ENTERPRISE_URL="https://TelenorSwedenAB.ghe.com"
export ORGANIZATION_NAME="TelenorSweden"

# Use the existing scale set that we know exists
export RUNNER_SCALE_SET_NAME="arc-runner-set"
export RUNNER_SCALE_SET_ID="1"
export RUNNER_GROUP_ID="1"
export RUNNER_LABELS="arc-runner-set"

# Basic AWS config (replace with your actual values)
export MIN_RUNNERS="0"
export MAX_RUNNERS="10"
export AWS_REGION="eu-north-1"
export EC2_SUBNET_ID="${EC2_SUBNET_ID:-subnet-placeholder}"
export EC2_SECURITY_GROUP_ID="${EC2_SECURITY_GROUP_ID:-sg-placeholder}"
export EC2_KEY_PAIR_NAME="${EC2_KEY_PAIR_NAME:-key-placeholder}"
export EC2_INSTANCE_TYPE="t3.medium"
export EC2_AMI_ID="${EC2_AMI_ID:-ami-placeholder}"
export EC2_SPOT_PRICE="0.05"

echo "Testing with existing scale set 'arc-runner-set'..."
echo ""
echo "This should work because:"
echo "1. ✅ Uses existing scale set (no creation needed)"
echo "2. ✅ Your token has read access"
echo "3. ✅ Will test message polling functionality"
echo ""

echo "After this works, you'll need to update your workflow from:"
echo "  runs-on: [self-hosted, linux, x64, ghalistener-managed]"
echo "To:"
echo "  runs-on: [arc-runner-set]"
echo ""

echo "Starting test..."
./ghaec2 