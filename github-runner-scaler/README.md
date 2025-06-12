# GitHub Runner Scaler Lambda

A serverless solution that automatically scales GitHub Actions runners using AWS Spot instances. This Lambda function polls GitHub Actions every 60 seconds to check for available jobs and creates AWS EC2 Spot instances when runners are needed.

## Architecture

```
┌─────────────────┐    ┌──────────────────┐    ┌─────────────────┐
│   EventBridge   │───▶│  Lambda Function │───▶│ GitHub Actions │
│  (every 60s)    │    │   (Go Runtime)   │    │      API        │
└─────────────────┘    └──────────────────┘    └─────────────────┘
                                │
                                ▼
                       ┌──────────────────┐    ┌─────────────────┐
                       │    DynamoDB      │    │  EC2 Spot       │
                       │ (Sessions/State) │    │   Instances     │
                       └──────────────────┘    └─────────────────┘
```

## Features

- **Automatic Scaling**: Monitors GitHub Actions job queue and scales runners accordingly
- **Cost Optimization**: Uses EC2 Spot instances for significant cost savings (up to 90% off)
- **Serverless**: No infrastructure to manage, only pay for compute time used
- **GitHub Integration**: Native integration with GitHub Actions scale sets
- **State Management**: Tracks runner state and sessions in DynamoDB
- **Configurable**: Support for custom runner labels, instance types, and scaling limits

## Prerequisites

1. **GitHub App**: Create a GitHub App with the following permissions:
   - Actions: Read
   - Administration: Read & Write
   - Metadata: Read

2. **AWS Account**: With permissions to create:
   - Lambda functions
   - EC2 instances
   - DynamoDB tables
   - IAM roles and policies
   - EventBridge rules

3. **GitHub Runner Scale Set**: Set up a runner scale set in your GitHub organization

## Setup

### 1. Clone and Configure

```bash
cd lambda/github-runner-scaler
```

### 2. Create terraform.tfvars

```hcl
# terraform/terraform.tfvars
aws_region                   = "us-east-1"
github_app_id               = "123456"
github_app_installation_id = "789012"
github_app_private_key      = <<EOF
-----BEGIN RSA PRIVATE KEY-----
your-private-key-here
-----END RSA PRIVATE KEY-----
EOF
runner_scale_set_id         = "1"
min_runners                 = 0
max_runners                 = 10
ec2_instance_type           = "t3.medium"
ec2_ami_id                 = "ami-0abcdef1234567890"  # Ubuntu 22.04 LTS
ec2_subnet_id              = "subnet-12345678"
ec2_key_pair_name          = "my-key-pair"
runner_labels              = ["self-hosted", "linux", "x64"]
```

### 3. Deploy

```bash
chmod +x deploy.sh
./deploy.sh
```

## Configuration

### Environment Variables

| Variable | Description | Required | Default |
|----------|-------------|----------|---------|
| `GITHUB_APP_ID` | GitHub App ID | Yes | - |
| `GITHUB_APP_INSTALLATION_ID` | Installation ID for your organization | Yes | - |
| `GITHUB_APP_PRIVATE_KEY` | Private key for GitHub App authentication | Yes | - |
| `RUNNER_SCALE_SET_ID` | ID of the runner scale set | Yes | - |
| `MIN_RUNNERS` | Minimum number of runners to maintain | No | 0 |
| `MAX_RUNNERS` | Maximum number of runners allowed | No | 10 |
| `EC2_INSTANCE_TYPE` | EC2 instance type for runners | No | t3.medium |
| `EC2_AMI_ID` | AMI ID for runner instances | Yes | - |
| `EC2_SUBNET_ID` | Subnet ID for instances | Yes | - |
| `EC2_SECURITY_GROUP_ID` | Security group for instances | Yes | - |
| `EC2_KEY_PAIR_NAME` | EC2 key pair name | Yes | - |
| `EC2_SPOT_PRICE` | Maximum spot price | No | 0.05 |
| `DYNAMODB_TABLE_NAME` | DynamoDB table for tracking | No | github-runners |
| `RUNNER_LABELS` | JSON array of runner labels | No | ["self-hosted","linux","x64"] |

### Scaling Logic

The Lambda function calculates the number of needed runners based on:

1. **Available Jobs**: Jobs waiting in the GitHub Actions queue
2. **Current Statistics**: Number of assigned vs registered runners
3. **Minimum Runners**: Maintain at least the configured minimum
4. **Maximum Runners**: Never exceed the configured maximum

Formula:
```
needed = available_jobs + assigned_jobs - registered_runners
needed = max(needed, min_runners)
needed = min(needed, max_runners)
```

## GitHub App Setup

### 1. Create GitHub App

1. Go to GitHub Settings > Developer settings > GitHub Apps
2. Click "New GitHub App"
3. Configure:
   - **Name**: `my-org-runner-scaler`
   - **Homepage URL**: Your organization URL
   - **Webhook**: Disable for now
   - **Permissions**:
     - Actions: Read
     - Administration: Read & Write
     - Metadata: Read
   - **Where can this GitHub App be installed?**: Only on this account

### 2. Generate Private Key

1. In your GitHub App settings, scroll to "Private keys"
2. Click "Generate a private key"
3. Download the `.pem` file and use its contents for `github_app_private_key`

### 3. Install the App

1. In your GitHub App settings, click "Install App"
2. Choose your organization
3. Select repositories or "All repositories"
4. Note the installation ID from the URL

## AMI Requirements

The EC2 AMI should have:

- Ubuntu 22.04 LTS (recommended)
- Docker installed and configured
- AWS CLI installed
- GitHub Actions runner dependencies

### Example AMI Creation Script

```bash
#!/bin/bash
# User data script for creating GitHub Actions runner AMI

# Update system
apt-get update -y
apt-get upgrade -y

# Install Docker
apt-get install -y docker.io
systemctl enable docker
systemctl start docker
usermod -aG docker ubuntu

# Install AWS CLI
curl "https://awscli.amazonaws.com/awscli-exe-linux-x86_64.zip" -o "awscliv2.zip"
unzip awscliv2.zip
./aws/install

# Install GitHub Actions runner dependencies
apt-get install -y curl jq git build-essential

# Create runner user
useradd -m -s /bin/bash runner
usermod -aG docker runner
```

## Monitoring

### CloudWatch Logs

Monitor Lambda execution logs:
```bash
aws logs tail /aws/lambda/github-runner-scaler --follow
```

### DynamoDB Tables

- `github-runners`: Tracks individual runner instances
- `github-runners-sessions`: Stores GitHub message session data

### CloudWatch Metrics

Key metrics to monitor:
- Lambda duration and errors
- EC2 Spot instance requests
- DynamoDB read/write units

## Troubleshooting

### Common Issues

1. **GitHub Authentication Errors**
   - Verify GitHub App private key format
   - Check installation ID is correct
   - Ensure app has required permissions

2. **EC2 Spot Request Failures**
   - Check spot price limits
   - Verify subnet and security group exist
   - Ensure AMI is available in the region

3. **Runner Registration Failures**
   - Verify AMI has required dependencies
   - Check security group allows outbound traffic
   - Ensure runner registration token is valid

### Debug Mode

Enable debug logging by setting CloudWatch log level to DEBUG:

```bash
aws logs put-retention-policy \
  --log-group-name /aws/lambda/github-runner-scaler \
  --retention-in-days 7
```

## Cost Optimization

### Spot Instance Savings

- Spot instances can save 50-90% compared to on-demand
- Configure appropriate spot price limits
- Monitor spot price history for your instance types

### Lambda Costs

- Function typically runs for 5-30 seconds
- EventBridge triggers every 60 seconds
- Estimated monthly cost: $5-15 for moderate usage

### DynamoDB Costs

- Pay-per-request pricing
- Minimal storage for session and runner state
- Estimated monthly cost: $1-5

## Security Considerations

1. **Private Key Storage**: Store GitHub App private key securely
2. **IAM Permissions**: Follow principle of least privilege
3. **VPC Configuration**: Place runners in private subnets
4. **Security Groups**: Restrict inbound access to necessary ports
5. **Instance Profiles**: Limit EC2 permissions to essential operations

## Contributing

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Test thoroughly
5. Submit a pull request

## License

This project is licensed under the MIT License - see the LICENSE file for details.

## Support

For issues and questions:
1. Check the troubleshooting section
2. Review CloudWatch logs
3. Open an issue with detailed error information 