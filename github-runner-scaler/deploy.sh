#!/bin/bash

set -e

# Build the Go binary
echo "Building Lambda function..."
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o main .

# Create deployment package
echo "Creating deployment package..."
zip github-runner-scaler.zip main

# Move to terraform directory
cd terraform

# Initialize Terraform if needed
if [ ! -d ".terraform" ]; then
    echo "Initializing Terraform..."
    terraform init
fi

# Plan the deployment
echo "Planning Terraform deployment..."
terraform plan

# Apply the deployment
echo "Deploying infrastructure..."
terraform apply -auto-approve

echo "Deployment completed successfully!"

# Clean up
cd ..
rm -f main github-runner-scaler.zip

echo "Lambda function deployed and scheduled to run every 60 seconds" 