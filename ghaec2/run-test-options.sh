#!/bin/bash

echo "=== GitHub Actions Scale Set Label Mismatch Solutions ==="
echo ""
echo "PROBLEM: Your job requests labels [self-hosted,linux,x64,ghalistener-managed]"
echo "         But existing scale set 'arc-runner-set' only has label [arc-runner-set]"
echo ""
echo "Choose one of these solutions:"
echo ""

# Option 1: Use existing scale set with different labels
echo "OPTION 1: Modify your workflow to use the existing scale set"
echo "Change your workflow from:"
echo "  runs-on: [self-hosted, linux, x64, ghalistener-managed]"
echo "To:"
echo "  runs-on: [arc-runner-set]"
echo ""

# Option 2: Create new scale set with correct labels
echo "OPTION 2: Create a new scale set with the labels your job expects"
echo "Run with environment variable to force new scale set creation:"
echo "export RUNNER_SCALE_SET_NAME=\"ghaec2-with-correct-labels\""
echo ""

# Option 3: Use the existing scale set by reusing its name
echo "OPTION 3: Use the existing scale set 'arc-runner-set'"
echo "Run with:"
echo "export RUNNER_SCALE_SET_NAME=\"arc-runner-set\""
echo "export RUNNER_LABELS=\"arc-runner-set\""
echo ""

echo "Which option do you want to test? (1, 2, or 3)"
read -p "Enter choice: " choice

case $choice in
  1)
    echo "Selected Option 1: You need to modify your workflow file"
    echo "Change the 'runs-on' in your .github/workflows/*.yml file to:"
    echo "runs-on: [arc-runner-set]"
    ;;
  2)
    echo "Selected Option 2: Creating new scale set with correct labels"
    export RUNNER_SCALE_SET_NAME="ghaec2-with-correct-labels"
    export RUNNER_LABELS="self-hosted,linux,x64,ghalistener-managed"
    echo "Using scale set name: $RUNNER_SCALE_SET_NAME"
    echo "Using labels: $RUNNER_LABELS"
    ;;
  3)
    echo "Selected Option 3: Using existing scale set 'arc-runner-set'"
    export RUNNER_SCALE_SET_NAME="arc-runner-set"
    export RUNNER_LABELS="arc-runner-set"
    echo "Using scale set name: $RUNNER_SCALE_SET_NAME"
    echo "Using labels: $RUNNER_LABELS"
    ;;
  *)
    echo "Invalid choice. Defaulting to Option 2."
    export RUNNER_SCALE_SET_NAME="ghaec2-with-correct-labels"
    export RUNNER_LABELS="self-hosted,linux,x64,ghalistener-managed"
    ;;
esac

# Set other required environment variables
export GITHUB_TOKEN="$GITHUB_TOKEN"
export GITHUB_ENTERPRISE_URL="https://TelenorSwedenAB.ghe.com"
export ORGANIZATION_NAME="TelenorSweden"
export MIN_RUNNERS="0"
export MAX_RUNNERS="10"
export AWS_REGION="eu-north-1"
export EC2_SUBNET_ID="$EC2_SUBNET_ID"
export EC2_SECURITY_GROUP_ID="$EC2_SECURITY_GROUP_ID"
export EC2_KEY_PAIR_NAME="$EC2_KEY_PAIR_NAME"
export EC2_INSTANCE_TYPE="t3.medium"
export EC2_AMI_ID="$EC2_AMI_ID"
export EC2_SPOT_PRICE="0.05"

echo ""
echo "Running ghaec2 with selected configuration..."
echo ""

./ghaec2 