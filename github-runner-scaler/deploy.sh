#!/bin/bash

set -e

# Build and deploy the GitHub Runner Scaler Lambda function

echo "🔧 Checking prerequisites..."

# Check if Go is installed
if ! command -v go &> /dev/null; then
    echo "❌ Go is not installed!"
    echo ""
    echo "📥 Install Go on macOS:"
    echo "   brew install go"
    echo ""
    echo "📥 Or download from: https://golang.org/dl/"
    echo ""
    echo "🔄 After installation, add to your PATH:"
    echo "   export PATH=\$PATH:/usr/local/go/bin"
    echo "   # Add this to your ~/.zshrc or ~/.bashrc"
    exit 1
fi

echo "✅ Go version: $(go version)"

echo "📦 Building Lambda function..."

# Clean up any previous builds
rm -f bootstrap main github-runner-scaler.zip

# Initialize go modules if needed
if [ ! -f "go.sum" ]; then
    echo "🔄 Initializing Go modules..."
    go mod tidy
fi

# Build for Linux (Lambda environment)
echo "🔨 Compiling for Linux..."
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="-s -w" -o bootstrap .

# Verify the binary was created
if [ ! -f "bootstrap" ]; then
    echo "❌ Build failed - bootstrap binary not found"
    exit 1
fi

echo "📦 Creating deployment package..."
# Ensure bootstrap has execute permissions
chmod +x bootstrap
# Create zip with proper permissions
zip -j github-runner-scaler.zip bootstrap

echo "✅ Build completed successfully!"
echo "📋 Package details:"
ls -lh github-runner-scaler.zip

if [ "$1" = "build-only" ]; then
    echo ""
    echo "🎯 Build completed. Package is ready for deployment"
    echo ""
    echo "🚀 Next steps:"
    echo "   1. cd terraform"
    echo "   2. terraform init"
    echo "   3. terraform plan"
    echo "   4. terraform apply"
    exit 0
fi

echo ""
echo "🚀 Deploying infrastructure..."

# Move to terraform directory
mv github-runner-scaler.zip terraform/
cd terraform

# Check if terraform is initialized
if [ ! -d ".terraform" ]; then
    echo "🔄 Initializing Terraform..."
    terraform init
fi

echo "📋 Planning deployment..."
terraform plan

echo ""
read -p "🤔 Do you want to apply these changes? (y/N): " -n 1 -r
echo
if [[ $REPLY =~ ^[Yy]$ ]]; then
    echo "✅ Deploying..."
    terraform apply -auto-approve
    echo ""
    echo "🎉 Deployment completed successfully!"
    echo ""
    echo "📊 Check CloudWatch logs:"
    echo "   aws logs tail /aws/lambda/github-runner-scaler --follow"
    echo ""
    echo "🔍 Monitor EC2 instances:"
    echo "   aws ec2 describe-instances --filters 'Name=tag:ManagedBy,Values=github-runner-scaler-lambda'"
else
    echo "❌ Deployment cancelled"
    exit 1
fi

# Clean up
cd ..
rm -f bootstrap main

echo ""
echo "🎯 Lambda function deployed and scheduled to run every 60 seconds"
echo "🔄 It will monitor for pending pipelines and create runners as needed" 