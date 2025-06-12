#!/bin/bash

echo "🚀 GHAEC2 Enhanced Job Detection Demo"
echo "======================================"
echo ""
echo "✅ What's been fixed:"
echo "  - Fallback mode implementation"
echo "  - Real job detection via GitHub API"
echo "  - Direct workflow runs monitoring"
echo "  - Enhanced scaling logic"
echo ""
echo "🔍 Now checking for your pipeline..."
echo ""

# Set environment variables (replace with your actual values)
export GITHUB_TOKEN="your-token-here"  # Update this!
export GITHUB_ENTERPRISE_URL="https://TelenorSwedenAB.ghe.com"
export ORGANIZATION_NAME="TelenorSweden"
export RUNNER_SCALE_SET_NAME="ghaec2-scaler"
export RUNNER_LABELS="self-hosted,linux,x64,ghalistener-managed"
export MIN_RUNNERS="0"
export MAX_RUNNERS="10"

# Mock AWS values for demo
export AWS_REGION="eu-north-1"
export EC2_SUBNET_ID="subnet-demo"
export EC2_SECURITY_GROUP_ID="sg-demo"
export EC2_KEY_PAIR_NAME="demo-key"
export EC2_INSTANCE_TYPE="t3.medium"
export EC2_AMI_ID="ami-demo"
export EC2_SPOT_PRICE="0.05"

echo "Expected behavior:"
echo "📊 Should show fallback mode activation"
echo "🔍 Should check TelenorSweden/test-spot-runner for workflow runs"
echo "⚡ Should detect your queued pipeline and scale up"
echo ""
echo "Starting application..."
echo ""

# Run the application with enhanced job detection
./ghaec2 