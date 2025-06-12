# GitHub Enterprise Server Runner Scaler Lambda

A serverless solution that automatically scales GitHub Actions self-hosted runners for GitHub Enterprise Server (GHE) using AWS Spot instances. This Lambda function monitors queued workflow runs every 60 seconds and creates AWS EC2 Spot instances when additional runners are needed.

## Architecture

```
┌─────────────────┐    ┌──────────────────┐    ┌─────────────────┐
│   EventBridge   │───▶│  Lambda Function │───▶│ GitHub Enterprise│
│  (every 60s)    │    │   (Go Runtime)   │    │     Server      │
└─────────────────┘    └──────────────────┘    └─────────────────┘
                                │
                                ▼
                       ┌──────────────────┐    ┌─────────────────┐
                       │    DynamoDB      │    │  EC2 Spot       │
                       │  (Runner State)  │    │   Instances     │
                       └──────────────────┘    └─────────────────┘
```

## Features

- **Automatic Scaling**: Monitors GitHub Enterprise workflow queue and scales runners accordingly
- **Cost Optimization**: Uses EC2 Spot instances for significant cost savings (up to 90% off)
- **Serverless**: No infrastructure to manage, only pay for compute time used
- **GHE Integration**: Native integration with GitHub Enterprise Server API
- **State Management**: Tracks runner state in DynamoDB
- **Intelligent Cleanup**: Automatically removes offline runners to optimize costs
- **Configurable**: Support for custom runner labels, instance types, and scaling limits

## Prerequisites

1. **GitHub Enterprise Server**: Access to your GHE instance with API access
2. **Personal Access Token**: GitHub token with `repo` and `admin:org` scopes
3. **AWS Account**: With permissions to create Lambda, EC2, DynamoDB, IAM resources
4. **VPC Setup**: Subnet and security group for EC2 instances

## Quick Start

### 1. Clone and Build

```bash
cd lambda/github-runner-scaler
./deploy.sh build-only
```

### 2. Create terraform.tfvars

```hcl
# terraform/terraform.tfvars
aws_region                  = "us-east-1"
github_token               = "ghp_xxxxxxxxxxxxxxxxxxxx"
github_enterprise_url      = "https://TelenorSwedenAB.ghe.com"
organization_name          = "TelenorSweden"
min_runners                = 1
max_runners                = 10
ec2_instance_type          = "t3.medium"
ec2_ami_id                = "ami-0abcdef1234567890"  # Ubuntu 22.04 LTS
ec2_subnet_id             = "subnet-12345678"
ec2_key_pair_name         = "my-key-pair"
runner_labels             = ["self-hosted", "linux", "x64"]
cleanup_offline_runners   = true
```

### 3. Deploy Infrastructure

```bash
cd terraform
terraform init
terraform plan
terraform apply
```

## Configuration Variables

### Required Variables

| Variable | Description | Example |
|----------|-------------|---------|
| `github_token` | Personal access token with repo and admin:org scopes | `ghp_xxxxxxxxxxxx` |
| `github_enterprise_url` | Your GHE instance URL | `https://github.company.com` |
| `organization_name` | GitHub organization name | `MyCompany` |
| `ec2_ami_id` | AMI ID with GitHub runner pre-installed | `ami-0abcdef123456` |
| `ec2_subnet_id` | VPC subnet for EC2 instances | `subnet-12345678` |

### Optional Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `aws_region` | AWS region for deployment | `us-east-1` |
| `min_runners` | Minimum runners to maintain | `1` |
| `max_runners` | Maximum runners allowed | `10` |
| `ec2_instance_type` | Instance type for runners | `t3.medium` |
| `ec2_key_pair_name` | EC2 key pair for SSH access | `""` |
| `runner_labels` | Labels for the runners | `["self-hosted", "linux", "x64"]` |
| `cleanup_offline_runners` | Remove offline runners | `true` |

## GitHub Token Setup

### 1. Create Personal Access Token

1. Go to your GHE instance: `https://your-ghe.com/settings/tokens`
2. Click "Generate new token"
3. Select scopes:
   - ✅ `repo` (Full control of private repositories)
   - ✅ `admin:org` (Full control of orgs and teams)
4. Copy the generated token

### 2. Test Token Access

```bash
# Test API access
curl -H "Authorization: token ghp_xxxxxxxxxxxx" \
  https://your-ghe.com/api/v3/orgs/YourOrg/actions/runners

# Test workflow runs access
curl -H "Authorization: token ghp_xxxxxxxxxxxx" \
  https://your-ghe.com/api/v3/orgs/YourOrg/actions/runs?status=queued
```

## AMI Setup

### Option 1: Use Pre-built AMI

Find an Ubuntu 22.04 LTS AMI with GitHub Actions runner pre-installed:

```bash
# Find Ubuntu 22.04 LTS AMI in your region
aws ec2 describe-images \
  --owners 099720109477 \
  --filters "Name=name,Values=ubuntu/images/hvm-ssd/ubuntu-jammy-22.04-amd64-server-*" \
  --query 'Images[*].[ImageId,Name,CreationDate]' \
  --output table
```

### Option 2: Create Custom AMI

Launch an EC2 instance and run:

```bash
#!/bin/bash
# User data script for GitHub Actions runner AMI

# Update system
sudo apt-get update -y
sudo apt-get upgrade -y

# Install Docker
sudo apt-get install -y docker.io
sudo systemctl enable docker
sudo systemctl start docker
sudo usermod -aG docker ubuntu

# Install dependencies
sudo apt-get install -y curl jq git build-essential unzip

# Install AWS CLI
curl "https://awscli.amazonaws.com/awscli-exe-linux-x86_64.zip" -o "awscliv2.zip"
unzip awscliv2.zip
sudo ./aws/install

# Install GitHub Actions runner
cd /opt
sudo mkdir actions-runner && cd actions-runner
sudo wget https://github.com/actions/runner/releases/download/v2.311.0/actions-runner-linux-x64-2.311.0.tar.gz
sudo tar xzf ./actions-runner-linux-x64-2.311.0.tar.gz
sudo ./bin/installdependencies.sh

# Create runner user
sudo useradd -m -s /bin/bash runner
sudo usermod -aG docker runner
sudo chown -R runner:runner /opt/actions-runner
```

Then create an AMI from this instance.

## Scaling Logic

The Lambda function uses intelligent scaling based on:

### 1. Demand Analysis
- Monitors queued workflow runs via GHE API
- Counts jobs waiting for available runners
- Analyzes current runner capacity and utilization

### 2. Supply Calculation
- Gets current registered runners from GHE
- Identifies idle vs busy runners
- Factors in offline runners for cleanup

### 3. Scaling Decision
```go
// Simplified scaling logic
queuedJobs := len(getQueuedWorkflowRuns())
idleRunners := getCurrentIdleRunners()
busyRunners := getCurrentBusyRunners()

needed := queuedJobs - idleRunners
if needed > 0 && (busyRunners + idleRunners) < maxRunners {
    createRunners(needed)
}
```

## Monitoring and Troubleshooting

### CloudWatch Logs

Monitor Lambda execution:
```bash
aws logs tail /aws/lambda/github-runner-scaler --follow
```

Key log messages to watch for:
- `✅ Found X queued workflow runs requiring runners`
- `🚀 Creating X new runners for pending jobs`
- `🧹 Cleaning up X offline runners`
- `⚠️ Rate limit reached, backing off`

### DynamoDB Tables

- **github-runners**: Tracks runner instances and their state
- **github-runners-sessions**: Stores API session data (if using runner scale sets)

### Common Issues

#### 1. No Runners Created
- Check GitHub token permissions
- Verify GHE API accessibility from Lambda
- Check VPC/subnet configuration for EC2 instances

#### 2. Authentication Errors
```bash
# Test token locally
curl -H "Authorization: token $GITHUB_TOKEN" \
  $GITHUB_ENTERPRISE_URL/api/v3/user
```

#### 3. Spot Instance Failures
- Check spot price limits
- Verify AMI ID exists in your region
- Ensure subnet has available IP addresses

### Testing

Use the included test workflows:

```bash
# Trigger test workflow
gh workflow run test-runner-scaling.yml

# Monitor scaling
aws logs tail /aws/lambda/github-runner-scaler --follow
```

## Cost Optimization

### Spot Instance Savings
- **On-Demand t3.medium**: ~$0.0416/hour
- **Spot t3.medium**: ~$0.0125/hour (70% savings)
- **Annual savings**: ~$268 per runner

### Lambda Costs
- **Execution time**: ~5 seconds per run
- **Memory**: 512MB
- **Monthly invocations**: ~43,200 (every minute)
- **Monthly cost**: ~$0.50

### Total Cost Example
For 5 average runners running 8 hours/day:
- **Spot instances**: $36.50/month
- **Lambda**: $0.50/month
- **DynamoDB**: $1.00/month
- **Total**: ~$38/month vs $150/month with on-demand

## Security Best Practices

1. **Minimal IAM Permissions**: Use least privilege principle
2. **VPC Security Groups**: Restrict network access
3. **Token Rotation**: Regularly rotate GitHub tokens
4. **Private Subnets**: Deploy runners in private subnets when possible
5. **Encryption**: Enable encryption for DynamoDB and EBS volumes

## Deployment Guide

See [DEPLOYMENT_GUIDE.md](DEPLOYMENT_GUIDE.md) for detailed setup instructions including:
- AWS account setup
- VPC configuration
- Security group setup
- AMI creation
- Terraform deployment

## Support

For issues and questions:
1. Check CloudWatch logs first
2. Verify configuration in terraform.tfvars
3. Test GitHub API access manually
4. Review security group and VPC settings

## License

MIT License - see [LICENSE](LICENSE) file for details. 