terraform {
  required_version = ">= 1.0"
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }
}

provider "aws" {
  region = var.aws_region
}

# Data sources
data "aws_caller_identity" "current" {}
data "aws_region" "current" {}

# Variables
variable "aws_region" {
  description = "AWS region"
  type        = string
  default     = "eu-north-1"
}

variable "github_token" {
  description = "GitHub Personal Access Token"
  type        = string
  sensitive   = true
}

variable "github_enterprise_url" {
  description = "GitHub Enterprise URL"
  type        = string
  default     = "https://TelenorSwedenAB.ghe.com"
}

variable "organization_name" {
  description = "GitHub organization name"
  type        = string
  default     = "TelenorSweden"
}

variable "runner_scale_set_name" {
  description = "Name for the runner scale set"
  type        = string
  default     = "ghalistener-ec2-scaler"
}

variable "runner_labels" {
  description = "Labels for the GitHub runners"
  type        = list(string)
  default     = ["self-hosted", "linux", "x64", "ghalistener-managed"]
}

variable "min_runners" {
  description = "Minimum number of runners"
  type        = number
  default     = 0
}

variable "max_runners" {
  description = "Maximum number of runners"
  type        = number
  default     = 10
}

variable "ec2_instance_type" {
  description = "EC2 instance type for runners"
  type        = string
  default     = "t3.medium"
}

variable "ec2_ami_id" {
  description = "AMI ID for EC2 instances"
  type        = string
}

variable "ec2_subnet_id" {
  description = "Subnet ID for EC2 instances"
  type        = string
}

variable "ec2_key_pair_name" {
  description = "Key pair name for EC2 instances"
  type        = string
}

variable "ec2_spot_price" {
  description = "Maximum spot price for EC2 instances"
  type        = string
  default     = "0.05"
}

variable "scaler_instance_type" {
  description = "Instance type for the scaler EC2 instance"
  type        = string
  default     = "t3.small"
}

# VPC and Security Group for the scaler
resource "aws_security_group" "scaler_sg" {
  name_description = "Security group for GHA Listener Scaler"
  
  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
  
  # Allow SSH access (optional, for debugging)
  ingress {
    from_port   = 22
    to_port     = 22
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }
  
  tags = {
    Name = "gha-listener-scaler-sg"
  }
}

# Security Group for runner instances
resource "aws_security_group" "runner_sg" {
  name_description = "Security group for GitHub Action runners"
  
  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
  
  tags = {
    Name = "gha-runner-sg"
  }
}

# IAM Role for the scaler EC2 instance
resource "aws_iam_role" "scaler_role" {
  name = "gha-listener-scaler-role"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Action = "sts:AssumeRole"
        Effect = "Allow"
        Principal = {
          Service = "ec2.amazonaws.com"
        }
      }
    ]
  })
}

# IAM Policy for the scaler
resource "aws_iam_role_policy" "scaler_policy" {
  name = "gha-listener-scaler-policy"
  role = aws_iam_role.scaler_role.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Action = [
          "ec2:DescribeInstances",
          "ec2:DescribeSpotInstanceRequests",
          "ec2:DescribeSpotPriceHistory",
          "ec2:RequestSpotInstances",
          "ec2:TerminateInstances",
          "ec2:CreateTags",
          "ec2:DescribeTags",
          "ec2:RunInstances",
          "ec2:DescribeImages",
          "ec2:DescribeSubnets",
          "ec2:DescribeSecurityGroups",
          "ec2:DescribeKeyPairs"
        ]
        Resource = "*"
      },
      {
        Effect = "Allow"
        Action = [
          "dynamodb:GetItem",
          "dynamodb:PutItem",
          "dynamodb:UpdateItem",
          "dynamodb:DeleteItem",
          "dynamodb:Query",
          "dynamodb:Scan"
        ]
        Resource = [
          aws_dynamodb_table.runner_state.arn,
          "${aws_dynamodb_table.runner_state.arn}/*"
        ]
      },
      {
        Effect = "Allow"
        Action = [
          "iam:PassRole"
        ]
        Resource = aws_iam_role.runner_role.arn
      }
    ]
  })
}

# IAM Instance Profile for the scaler
resource "aws_iam_instance_profile" "scaler_profile" {
  name = "gha-listener-scaler-profile"
  role = aws_iam_role.scaler_role.name
}

# IAM Role for runner instances
resource "aws_iam_role" "runner_role" {
  name = "gha-runner-role"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Action = "sts:AssumeRole"
        Effect = "Allow"
        Principal = {
          Service = "ec2.amazonaws.com"
        }
      }
    ]
  })
}

# IAM Instance Profile for runners
resource "aws_iam_instance_profile" "runner_profile" {
  name = "gha-runner-profile"
  role = aws_iam_role.runner_role.name
}

# DynamoDB table for runner state
resource "aws_dynamodb_table" "runner_state" {
  name           = "gha-runner-state"
  billing_mode   = "PAY_PER_REQUEST"
  hash_key       = "InstanceId"

  attribute {
    name = "InstanceId"
    type = "S"
  }

  attribute {
    name = "Status"
    type = "S"
  }

  global_secondary_index {
    name     = "StatusIndex"
    hash_key = "Status"
  }

  tags = {
    Name = "gha-runner-state"
  }
}

# User data script for the scaler instance
locals {
  scaler_user_data = base64encode(templatefile("${path.module}/scaler-userdata.sh", {
    github_token           = var.github_token
    github_enterprise_url  = var.github_enterprise_url
    organization_name      = var.organization_name
    runner_scale_set_name  = var.runner_scale_set_name
    runner_labels          = join(",", var.runner_labels)
    min_runners           = var.min_runners
    max_runners           = var.max_runners
    aws_region            = var.aws_region
    ec2_subnet_id         = var.ec2_subnet_id
    ec2_security_group_id = aws_security_group.runner_sg.id
    ec2_key_pair_name     = var.ec2_key_pair_name
    ec2_instance_type     = var.ec2_instance_type
    ec2_ami_id            = var.ec2_ami_id
    ec2_spot_price        = var.ec2_spot_price
    runner_instance_profile = aws_iam_instance_profile.runner_profile.name
  }))
}

# EC2 instance for the scaler
resource "aws_instance" "scaler" {
  ami                    = var.ec2_ami_id
  instance_type          = var.scaler_instance_type
  key_name              = var.ec2_key_pair_name
  subnet_id             = var.ec2_subnet_id
  vpc_security_group_ids = [aws_security_group.scaler_sg.id]
  iam_instance_profile   = aws_iam_instance_profile.scaler_profile.name
  
  user_data = local.scaler_user_data
  
  tags = {
    Name = "gha-listener-scaler"
    Type = "scaler"
  }
  
  # Ensure the instance stays running
  disable_api_termination = false
  
  root_block_device {
    volume_type = "gp3"
    volume_size = 20
    encrypted   = true
  }
}

# CloudWatch Log Group for the scaler
resource "aws_cloudwatch_log_group" "scaler_logs" {
  name              = "/aws/ec2/gha-listener-scaler"
  retention_in_days = 14
}

# Outputs
output "scaler_instance_id" {
  description = "ID of the scaler EC2 instance"
  value       = aws_instance.scaler.id
}

output "scaler_public_ip" {
  description = "Public IP of the scaler EC2 instance"
  value       = aws_instance.scaler.public_ip
}

output "scaler_private_ip" {
  description = "Private IP of the scaler EC2 instance"
  value       = aws_instance.scaler.private_ip
}

output "runner_security_group_id" {
  description = "Security Group ID for runner instances"
  value       = aws_security_group.runner_sg.id
}

output "runner_instance_profile_name" {
  description = "IAM Instance Profile name for runner instances"
  value       = aws_iam_instance_profile.runner_profile.name
}

output "dynamodb_table_name" {
  description = "DynamoDB table name for runner state"
  value       = aws_dynamodb_table.runner_state.name
}

output "cloudwatch_log_group" {
  description = "CloudWatch log group for scaler logs"
  value       = aws_cloudwatch_log_group.scaler_logs.name
} 