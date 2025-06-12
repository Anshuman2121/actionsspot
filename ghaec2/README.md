# 🚀 **GHAEC2 - GitHub Actions EC2 Scaler**

A simplified, manual EC2-based GitHub Actions runner scaler that uses the GitHub Actions Service API (similar to actions-runner-controller's ghalistener) for real-time job detection and scaling.

## 🌟 **Key Features**

- ✅ **Real-time job detection** using GitHub Actions Service API
- ✅ **WebSocket-like message polling** (2-second intervals)
- ✅ **Label-based job matching** with exact ARC algorithm
- ✅ **EC2 spot instance management** for cost optimization
- ✅ **No DynamoDB** - simple and lightweight
- ✅ **No Terraform** - manual setup for full control

## 📋 **Prerequisites**

- ✅ AWS CLI configured with EC2 permissions
- ✅ Go 1.21+ installed
- ✅ GitHub Enterprise Server access with admin permissions
- ✅ An AWS VPC with subnets and internet access

## 🏗️ **Step 1: AWS Infrastructure Setup**

### 1.1 Create Security Group

```bash
# Create security group for the scaler instance
aws ec2 create-security-group \
  --group-name ghaec2-scaler-sg \
  --description "Security group for GHAEC2 scaler" \
  --vpc-id vpc-xxxxxxxxx

# Allow SSH access (replace with your IP)
aws ec2 authorize-security-group-ingress \
  --group-id sg-xxxxxxxxx \
  --protocol tcp \
  --port 22 \
  --cidr 0.0.0.0/0

# Allow outbound internet access (if not default)
aws ec2 authorize-security-group-egress \
  --group-id sg-xxxxxxxxx \
  --protocol -1 \
  --port -1 \
  --cidr 0.0.0.0/0
```

### 1.2 Create Security Group for Runner Instances

```bash
# Create security group for runner instances
aws ec2 create-security-group \
  --group-name ghaec2-runners-sg \
  --description "Security group for GHAEC2 runner instances" \
  --vpc-id vpc-xxxxxxxxx

# Allow outbound internet access
aws ec2 authorize-security-group-egress \
  --group-id sg-yyyyyyyyy \
  --protocol -1 \
  --port -1 \
  --cidr 0.0.0.0/0
```

### 1.3 Create IAM Role for Scaler Instance

```bash
# Create trust policy
cat > trust-policy.json << EOF
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Principal": {
        "Service": "ec2.amazonaws.com"
      },
      "Action": "sts:AssumeRole"
    }
  ]
}
EOF

# Create IAM role
aws iam create-role \
  --role-name ghaec2-scaler-role \
  --assume-role-policy-document file://trust-policy.json

# Create permissions policy
cat > permissions-policy.json << EOF
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "ec2:DescribeInstances",
        "ec2:DescribeSpotInstanceRequests",
        "ec2:RequestSpotInstances",
        "ec2:TerminateInstances",
        "ec2:CreateTags",
        "ec2:DescribeSpotPriceHistory",
        "ec2:DescribeImages",
        "ec2:DescribeSnapshots",
        "ec2:DescribeKeyPairs",
        "ec2:DescribeSecurityGroups",
        "ec2:DescribeSubnets",
        "ec2:DescribeVpcs"
      ],
      "Resource": "*"
    }
  ]
}
EOF

# Attach permissions policy
aws iam put-role-policy \
  --role-name ghaec2-scaler-role \
  --policy-name ghaec2-scaler-permissions \
  --policy-document file://permissions-policy.json

# Create instance profile
aws iam create-instance-profile \
  --instance-profile-name ghaec2-scaler-profile

# Add role to instance profile
aws iam add-role-to-instance-profile \
  --instance-profile-name ghaec2-scaler-profile \
  --role-name ghaec2-scaler-role

# Clean up
rm trust-policy.json permissions-policy.json
```

### 1.4 Create Key Pair (if needed)

```bash
aws ec2 create-key-pair \
  --key-name ghaec2-key \
  --query 'KeyMaterial' \
  --output text > ghaec2-key.pem

chmod 400 ghaec2-key.pem
```

## 🚀 **Step 2: Create Scaler EC2 Instance**

### 2.1 Find Latest Ubuntu AMI

```bash
aws ec2 describe-images \
  --owners 099720109477 \
  --filters "Name=name,Values=ubuntu/images/hvm-ssd/ubuntu-jammy-22.04-amd64-server-*" \
  --query 'Images | sort_by(@, &CreationDate) | [-1].ImageId' \
  --output text
```

### 2.2 Launch Scaler Instance

```bash
aws ec2 run-instances \
  --image-id ami-xxxxxxxxx \
  --count 1 \
  --instance-type t3.medium \
  --key-name ghaec2-key \
  --security-group-ids sg-xxxxxxxxx \
  --subnet-id subnet-xxxxxxxxx \
  --iam-instance-profile Name=ghaec2-scaler-profile \
  --tag-specifications 'ResourceType=instance,Tags=[{Key=Name,Value=ghaec2-scaler},{Key=Type,Value=ghaec2-scaler}]'
```

### 2.3 Get Instance Details

```bash
aws ec2 describe-instances \
  --filters "Name=tag:Type,Values=ghaec2-scaler" \
  --query 'Reservations[0].Instances[0].{InstanceId:InstanceId,PublicIp:PublicIpAddress,PrivateIp:PrivateIpAddress,State:State.Name}' \
  --output table
```

## 🔧 **Step 3: Setup Scaler Instance**

### 3.1 SSH into Instance

```bash
ssh -i ghaec2-key.pem ubuntu@YOUR_INSTANCE_PUBLIC_IP
```

### 3.2 Install Dependencies

```bash
# Update system
sudo apt update && sudo apt upgrade -y

# Install Go
wget https://go.dev/dl/go1.21.5.linux-amd64.tar.gz
sudo tar -C /usr/local -xzf go1.21.5.linux-amd64.tar.gz
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
source ~/.bashrc

# Install git and other tools
sudo apt install -y git curl wget jq

# Verify Go installation
go version
```

### 3.3 Clone and Build Application

```bash
# Create working directory
mkdir -p ~/ghaec2
cd ~/ghaec2

# Copy the Go files (you'll need to transfer these)
# Option 1: Use git clone if this is in a repository
# Option 2: Use scp to copy files
# Option 3: Create files manually (shown below)
```

### 3.4 Transfer Application Files

**Option A: Using SCP (from your local machine)**

```bash
# From your local machine
scp -i ghaec2-key.pem ghaec2/*.go ubuntu@YOUR_INSTANCE_PUBLIC_IP:~/ghaec2/
scp -i ghaec2-key.pem ghaec2/go.mod ubuntu@YOUR_INSTANCE_PUBLIC_IP:~/ghaec2/
```

**Option B: Create files on the instance** (if files are too large for copy-paste)

```bash
# You would create each .go file using nano or vim
# nano ~/ghaec2/main.go
# nano ~/ghaec2/gha_actions_client.go
# nano ~/ghaec2/scaler.go
# nano ~/ghaec2/go.mod
```

### 3.5 Build Application

```bash
cd ~/ghaec2

# Initialize Go modules
go mod tidy

# Build the application
go build -o ghaec2 .

# Make it executable
chmod +x ghaec2
```

## ⚙️ **Step 4: Configuration**

### 4.1 Create Environment File

```bash
cat > ~/ghaec2/.env << EOF
# GitHub Configuration
GITHUB_TOKEN="ghp_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"
GITHUB_ENTERPRISE_URL="https://TelenorSwedenAB.ghe.com"
ORGANIZATION_NAME="TelenorSweden"

# Runner Configuration
RUNNER_LABELS="self-hosted,linux,x64,ghalistener-managed"
MIN_RUNNERS=0
MAX_RUNNERS=10
RUNNER_SCALE_SET_NAME="ghaec2-scaler"

# AWS Configuration
AWS_REGION="eu-north-1"
EC2_SUBNET_ID="subnet-xxxxxxxxx"
EC2_SECURITY_GROUP_ID="sg-yyyyyyyyy"  # Runner security group
EC2_KEY_PAIR_NAME="ghaec2-key"
EC2_INSTANCE_TYPE="t3.medium"
EC2_AMI_ID="ami-xxxxxxxxx"  # Ubuntu 22.04 AMI
EC2_SPOT_PRICE="0.05"
EOF

# Make sure the file is secure
chmod 600 ~/ghaec2/.env
```

### 4.2 Create Systemd Service

```bash
sudo cat > /etc/systemd/system/ghaec2.service << EOF
[Unit]
Description=GitHub Actions EC2 Scaler
After=network.target
Wants=network-online.target

[Service]
Type=simple
User=ubuntu
WorkingDirectory=/home/ubuntu/ghaec2
EnvironmentFile=/home/ubuntu/ghaec2/.env
ExecStart=/home/ubuntu/ghaec2/ghaec2
Restart=always
RestartSec=5
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
EOF

# Reload systemd and enable service
sudo systemctl daemon-reload
sudo systemctl enable ghaec2
```

## 🎯 **Step 5: Testing**

### 5.1 Test Configuration

```bash
cd ~/ghaec2

# Source environment variables
source .env

# Test the application
./ghaec2
```

### 5.2 Check AWS Permissions

```bash
# Test EC2 permissions
aws ec2 describe-instances --max-items 1

# Test spot pricing
aws ec2 describe-spot-price-history \
  --instance-types t3.medium \
  --product-descriptions "Linux/UNIX" \
  --max-items 5
```

### 5.3 Test GitHub Token

```bash
curl -H "Authorization: token $GITHUB_TOKEN" \
  $GITHUB_ENTERPRISE_URL/api/v3/user
```

## 🚀 **Step 6: Start Service**

### 6.1 Start the Service

```bash
sudo systemctl start ghaec2
```

### 6.2 Check Status

```bash
# Check service status
sudo systemctl status ghaec2

# Check logs
sudo journalctl -u ghaec2 -f
```

### 6.3 Monitor Operation

```bash
# Watch real-time logs
sudo journalctl -u ghaec2 -f

# Check running instances
aws ec2 describe-instances \
  --filters "Name=tag:Type,Values=ghaec2-runner" \
  --query 'Reservations[].Instances[].{Name:Tags[?Key==`Name`].Value|[0],State:State.Name,InstanceId:InstanceId}' \
  --output table
```

## 📊 **Step 7: Verification**

### 7.1 Trigger a Workflow

1. Create a workflow in your GitHub repository with matching labels:

```yaml
name: Test GHAEC2
on: workflow_dispatch
jobs:
  test:
    runs-on: [self-hosted, linux, x64, ghalistener-managed]
    steps:
      - name: Test
        run: echo "Hello from GHAEC2 runner!"
```

2. Trigger the workflow manually
3. Watch the scaler logs for job detection
4. Verify that a runner instance is created

### 7.2 Check Scaler Logs

```bash
# Look for these log messages
sudo journalctl -u ghaec2 -f | grep -E "(Job available|Creating runner|Spot instance requested)"
```

## 🛠️ **Troubleshooting**

### Common Issues

#### 🔴 **"Failed to get registration token"**
```bash
# Test GitHub connectivity
curl -v -H "Authorization: token $GITHUB_TOKEN" \
  $GITHUB_ENTERPRISE_URL/api/v3/user
```

#### 🔴 **"Failed to request spot instance"**
```bash
# Check spot pricing
aws ec2 describe-spot-price-history \
  --instance-types t3.medium \
  --product-descriptions "Linux/UNIX" \
  --max-items 5

# Check subnet
aws ec2 describe-subnets --subnet-ids $EC2_SUBNET_ID
```

#### 🔴 **"Permission denied"**
```bash
# Check IAM role
aws sts get-caller-identity

# Check instance profile
aws ec2 describe-instances --instance-ids i-xxxxxxxxx \
  --query 'Reservations[0].Instances[0].IamInstanceProfile'
```

### Debug Mode

```bash
# Run in debug mode
cd ~/ghaec2
source .env
./ghaec2 2>&1 | tee debug.log
```

## 🧹 **Cleanup**

### Stop and Remove Service

```bash
# Stop service
sudo systemctl stop ghaec2
sudo systemctl disable ghaec2

# Remove service file
sudo rm /etc/systemd/system/ghaec2.service
sudo systemctl daemon-reload
```

### Terminate All Runner Instances

```bash
# Get all runner instances
aws ec2 describe-instances \
  --filters "Name=tag:Type,Values=ghaec2-runner" \
  --query 'Reservations[].Instances[].InstanceId' \
  --output text | xargs aws ec2 terminate-instances --instance-ids
```

### Remove AWS Resources

```bash
# Terminate scaler instance
aws ec2 terminate-instances --instance-ids i-xxxxxxxxx

# Delete security groups
aws ec2 delete-security-group --group-id sg-xxxxxxxxx
aws ec2 delete-security-group --group-id sg-yyyyyyyyy

# Delete IAM resources
aws iam remove-role-from-instance-profile \
  --instance-profile-name ghaec2-scaler-profile \
  --role-name ghaec2-scaler-role

aws iam delete-instance-profile --instance-profile-name ghaec2-scaler-profile
aws iam delete-role-policy --role-name ghaec2-scaler-role --policy-name ghaec2-scaler-permissions
aws iam delete-role --role-name ghaec2-scaler-role

# Delete key pair
aws ec2 delete-key-pair --key-name ghaec2-key
rm ghaec2-key.pem
```

## 📈 **Scaling & Performance**

- **Real-time Detection**: ~1-2 seconds from job queue to runner creation
- **Cost**: $15-25/month for scaler instance + spot instance costs
- **Capacity**: Configurable via `MAX_RUNNERS` (default: 10)
- **Reliability**: Auto-restart service on failure

## 🔒 **Security Notes**

- GitHub token is stored in environment file (secure with 600 permissions)
- IAM roles provide EC2 permissions without hardcoded credentials
- Security groups restrict network access
- Spot instances are automatically tagged for identification

## 📝 **Next Steps**

1. Monitor logs for first workflow run
2. Adjust `MAX_RUNNERS` based on usage
3. Optimize spot pricing based on regional costs
4. Add CloudWatch logging for production monitoring
5. Consider implementing cleanup mechanisms for stale runners 