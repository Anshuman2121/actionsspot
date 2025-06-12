# 🚀 **GitHub Runner Scaler - Deployment Guide**

This guide will help you deploy the GitHub Runner Scaler Lambda function using Terraform.

## 📋 **Prerequisites**

- ✅ AWS CLI configured with appropriate permissions
- ✅ Terraform installed (>= 1.0)
- ✅ GitHub Enterprise Server access with admin permissions
- ✅ An AWS VPC with subnets and internet access

## 🔧 **Required Variables & How to Get Them**

### 🔐 **GitHub Configuration**

#### 1. **GitHub Personal Access Token** (`github_token`)
**How to get it:**
1. Go to your GitHub Enterprise: `https://TelenorSwedenAB.ghe.com`
2. Click your profile picture → **Settings**
3. Go to **Developer settings** → **Personal access tokens** → **Tokens (classic)**
4. Click **Generate new token (classic)**
5. Select these scopes:
   ```
   ✅ repo (Full control of private repositories)
   ✅ admin:org (Full control of orgs and teams)
   ✅ admin:repo_hook (Admin repo hooks)
   ✅ workflow (Update GitHub Action workflows)
   ```
6. Copy the generated token (starts with `ghp_`)

**Example:**
```bash
export TF_VAR_github_token="ghp_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"
```

#### 2. **GitHub Enterprise URL** (`github_enterprise_url`)
**Default:** `https://TelenorSwedenAB.ghe.com`

**How to verify:**
```bash
curl -H "Authorization: token YOUR_TOKEN" \
  https://TelenorSwedenAB.ghe.com/api/v3/meta
```

#### 3. **Organization Name** (`organization_name`)
**Default:** `TelenorSweden`

**How to verify:**
```bash
curl -H "Authorization: token YOUR_TOKEN" \
  https://TelenorSwedenAB.ghe.com/api/v3/orgs/TelenorSweden
```

### 🏗️ **AWS Infrastructure Configuration**

#### 4. **AWS Region** (`aws_region`)
**How to get it:**
```bash
aws configure get region
# Example: us-east-1, eu-west-1, etc.
```

#### 5. **EC2 AMI ID** (`ec2_ami_id`)
**How to get it:**
Find the latest Ubuntu 20.04 LTS AMI in your region:
```bash
aws ec2 describe-images \
  --owners 099720109477 \
  --filters "Name=name,Values=ubuntu/images/hvm-ssd/ubuntu-focal-20.04-amd64-server-*" \
  --query 'Images | sort_by(@, &CreationDate) | [-1].ImageId' \
  --output text
```

**Example output:** `ami-0c02fb55956c7d316`

#### 6. **EC2 Subnet ID** (`ec2_subnet_id`)
**How to get it:**
List subnets in your VPC:
```bash
aws ec2 describe-subnets \
  --filters "Name=vpc-id,Values=YOUR_VPC_ID" \
  --query 'Subnets[?MapPublicIpOnLaunch==`true`].[SubnetId,AvailabilityZone,CidrBlock]' \
  --output table
```

**Choose a public subnet with internet access for runner instances.**

#### 7. **EC2 Key Pair Name** (`ec2_key_pair_name`)
**How to get it:**
List existing key pairs:
```bash
aws ec2 describe-key-pairs --query 'KeyPairs[].KeyName' --output table
```

**Or create a new one:**
```bash
aws ec2 create-key-pair \
  --key-name github-runners-key \
  --query 'KeyMaterial' \
  --output text > github-runners-key.pem
chmod 400 github-runners-key.pem
```

#### 8. **EC2 Instance Type** (`ec2_instance_type`)
**Recommended values:**
- `t3.micro` - Testing/light workloads
- `t3.small` - Small projects  
- `t3.medium` - **Recommended** (default)
- `t3.large` - Heavy workloads
- `c5.large` - CPU-intensive tasks

### 🏃 **Runner Configuration**

#### 9. **Min/Max Runners** (`min_runners`, `max_runners`)
**Recommended:**
```hcl
min_runners = 0    # Cost optimization
max_runners = 10   # Adjust based on your needs
```

#### 10. **Runner Labels** (`runner_labels`)
**Default:** `["self-hosted", "linux", "x64", "lambda-managed"]`

**Custom labels example:**
```hcl
runner_labels = [
  "self-hosted",
  "linux", 
  "x64",
  "lambda-managed",
  "telenor",
  "production"
]
```

#### 11. **Repository Names** (`repository_names`) - OPTIONAL
**What it is:** List of specific repositories to monitor for workflow runs.
**Default:** If not specified, monitors ALL repositories in the organization.

**How to configure:**
```hcl
# Monitor specific repositories only
repository_names = [
  "my-app-repo",
  "infrastructure-repo",
  "api-service"
]

# Or monitor repositories from different organizations
repository_names = [
  "TelenorSweden/my-app-repo",
  "TelenorSweden/another-repo"
]
```

**Note:** This is recommended for large organizations with many repositories to improve performance and reduce API calls.

## 🎯 **Step-by-Step Deployment**

### **Step 1: Clone and Prepare**
```bash
cd lambda/github-runner-scaler/terraform
```

### **Step 2: Create terraform.tfvars**
```bash
cat > terraform.tfvars << EOF
# AWS Configuration
aws_region = "us-east-1"
ec2_ami_id = "ami-0c02fb55956c7d316"
ec2_subnet_id = "subnet-xxxxxxxxx"
ec2_key_pair_name = "github-runners-key"
ec2_instance_type = "t3.medium"

# GitHub Configuration  
github_token = "ghp_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"
github_enterprise_url = "https://TelenorSwedenAB.ghe.com"
organization_name = "TelenorSweden"

# Runner Configuration
min_runners = 0
max_runners = 10
runner_labels = ["self-hosted", "linux", "x64", "lambda-managed"]
cleanup_offline_runners = true

# Optional: Monitor specific repositories only (improves performance)
# repository_names = ["my-app-repo", "api-service", "infrastructure"]
EOF
```

### **Step 3: Build Lambda Package**
```bash
cd ..  # Back to lambda/github-runner-scaler/
./deploy.sh build-only
cd terraform/
```

### **Step 4: Initialize Terraform**
```bash
terraform init
```

### **Step 5: Plan Deployment**
```bash
terraform plan
```

### **Step 6: Deploy**
```bash
terraform apply
```

## 🧪 **Testing the Deployment**

### **Test 1: Check Lambda Function**
```bash
aws lambda invoke \
  --function-name github-runner-scaler \
  --payload '{}' \
  response.json

cat response.json
```

### **Test 2: Check CloudWatch Logs**
```bash
aws logs describe-log-streams \
  --log-group-name "/aws/lambda/github-runner-scaler" \
  --order-by LastEventTime \
  --descending

aws logs get-log-events \
  --log-group-name "/aws/lambda/github-runner-scaler" \
  --log-stream-name "LATEST_STREAM_NAME"
```

### **Test 3: Manual Pipeline Monitoring Test**
```bash
# From the lambda directory
export GITHUB_TOKEN="your_token_here"
export EC2_SUBNET_ID="subnet-xxxxxxxxx"
export EC2_SECURITY_GROUP_ID="sg-xxxxxxxxx"
export EC2_KEY_PAIR_NAME="github-runners-key"

go run . test
```

## 📊 **Monitoring & Troubleshooting**

### **CloudWatch Dashboards**
- **Log Group:** `/aws/lambda/github-runner-scaler`
- **Metrics:** Lambda execution time, errors, throttles
- **Custom Metrics:** Runner creation/termination events

### **Common Issues**

#### 🔴 **"Failed to connect to GHE"**
```bash
# Test connectivity
curl -H "Authorization: token YOUR_TOKEN" \
  https://TelenorSwedenAB.ghe.com/api/v3/user
```

#### 🔴 **"Not Found" (HTTP 404) for workflow runs**
This is likely because you need to configure specific repositories:
```bash
# Add repository names to your terraform.tfvars
repository_names = ["repo1", "repo2", "repo3"]
```

Or test a specific repository:
```bash
curl -H "Authorization: token YOUR_TOKEN" \
  https://TelenorSwedenAB.ghe.com/api/v3/repos/TelenorSweden/REPO_NAME/actions/runs
```

#### 🔴 **"No spot instances created"**
```bash
# Check spot pricing
aws ec2 describe-spot-price-history \
  --instance-types t3.medium \
  --product-descriptions "Linux/UNIX" \
  --max-items 5
```

#### 🔴 **"Permission denied"**
```bash
# Check IAM permissions
aws sts get-caller-identity
aws iam list-attached-role-policies --role-name github-runner-scaler-lambda-role
```

#### 🔴 **"Subnet not found"**
```bash
# Verify subnet
aws ec2 describe-subnets --subnet-ids subnet-xxxxxxxxx
```

## 🧹 **Cleanup**

To destroy all resources:
```bash
terraform destroy
```

## 📈 **Scaling Considerations**

- **EventBridge Schedule:** Currently set to 1 minute intervals
- **Lambda Timeout:** 15 minutes (900 seconds)
- **Spot Instance Pricing:** $0.05/hour maximum
- **DynamoDB:** Pay-per-request billing mode

## 🔧 **Advanced Configuration**

### **Custom EventBridge Schedule**
Edit `schedule_expression` in `main.tf`:
```hcl
schedule_expression = "rate(2 minutes)"  # Every 2 minutes
schedule_expression = "rate(30 seconds)" # Every 30 seconds  
schedule_expression = "cron(0 */5 * * ? *)" # Every 5 minutes
```

### **Custom Spot Pricing**
Update the spot price in `main.tf`:
```hcl
EC2_SPOT_PRICE = "0.10"  # $0.10/hour maximum
```

### **Multi-Region Deployment**
Deploy in multiple regions for high availability:
```bash
# Region 1
terraform apply -var="aws_region=us-east-1"

# Region 2  
terraform apply -var="aws_region=eu-west-1"
``` 