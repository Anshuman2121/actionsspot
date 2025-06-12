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
data "aws_subnet" "selected" {
  id = var.subnet_id
}

# Get latest Ubuntu 22.04 LTS AMI
data "aws_ami" "ubuntu" {
  most_recent = true
  owners      = ["099720109477"] # Canonical
  
  filter {
    name   = "name"
    values = ["ubuntu/images/hvm-ssd/ubuntu-jammy-22.04-amd64-server-*"]
  }
  
  filter {
    name   = "virtualization-type"
    values = ["hvm"]
  }
}

# 1.1 & 1.2 Security Groups
resource "aws_security_group" "scaler" {
  name_prefix = "ghaec2-scaler-"
  description = "Security group for GHAEC2 scaler instance"
  vpc_id      = data.aws_subnet.selected.vpc_id

  # Outbound internet access
  egress {
    description = "All outbound traffic"
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = {
    Name = "ghaec2-scaler-sg"
    Type = "ghaec2-scaler"
  }
}

resource "aws_security_group" "runners" {
  name_prefix = "ghaec2-runners-"
  description = "Security group for GHAEC2 runner instances"
  vpc_id      = data.aws_subnet.selected.vpc_id

  # Outbound internet access
  egress {
    description = "All outbound traffic"
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = {
    Name = "ghaec2-runners-sg"
    Type = "ghaec2-runners"
  }
}

# 1.3 IAM Role for Scaler Instance
resource "aws_iam_role" "scaler_role" {
  name = "ghaec2-scaler-role"

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

  tags = {
    Name = "ghaec2-scaler-role"
    Type = "ghaec2-scaler"
  }
}

resource "aws_iam_role_policy" "scaler_policy" {
  name = "ghaec2-scaler-permissions"
  role = aws_iam_role.scaler_role.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Action = [
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
        ]
        Resource = "*"
      },
      {
        Effect = "Allow"
        Action = [
          "ssm:UpdateInstanceInformation",
          "ssmmessages:CreateControlChannel",
          "ssmmessages:CreateDataChannel",
          "ssmmessages:OpenControlChannel",
          "ssmmessages:OpenDataChannel"
        ]
        Resource = "*"
      }
    ]
  })
}

resource "aws_iam_instance_profile" "scaler_profile" {
  name = "ghaec2-scaler-profile"
  role = aws_iam_role.scaler_role.name

  tags = {
    Name = "ghaec2-scaler-profile"
    Type = "ghaec2-scaler"
  }
}

# 1.4 Key Pair (optional - create if not provided)
resource "aws_key_pair" "ghaec2" {
  count      = var.key_pair_name == "" ? 1 : 0
  key_name   = "ghaec2-key"
  public_key = var.public_key

  tags = {
    Name = "ghaec2-key"
    Type = "ghaec2"
  }
}

locals {
  key_pair_name = var.key_pair_name != "" ? var.key_pair_name : aws_key_pair.ghaec2[0].key_name
}

# User data script for scaler instance
locals {
  scaler_user_data = base64encode(templatefile("${path.module}/scaler-userdata.sh", {
    github_token           = var.github_token
    github_enterprise_url  = var.github_enterprise_url
    organization_name      = var.organization_name
    runner_labels          = var.runner_labels
    min_runners           = var.min_runners
    max_runners           = var.max_runners
    runner_scale_set_name = var.runner_scale_set_name
    aws_region            = var.aws_region
    subnet_id             = var.subnet_id
    security_group_id     = aws_security_group.runners.id
    key_pair_name         = local.key_pair_name
    instance_type         = var.runner_instance_type
    ami_id                = data.aws_ami.ubuntu.id
    spot_price            = var.spot_price
  }))
}

# 2.2 Launch Scaler Instance
resource "aws_instance" "scaler" {
  ami                     = data.aws_ami.ubuntu.id
  instance_type           = var.scaler_instance_type
  key_name               = local.key_pair_name
  vpc_security_group_ids = [aws_security_group.scaler.id]
  subnet_id              = var.subnet_id
  iam_instance_profile   = aws_iam_instance_profile.scaler_profile.name
  
  user_data = local.scaler_user_data

  root_block_device {
    volume_type = "gp3"
    volume_size = 20
    encrypted   = true
    
    tags = {
      Name = "ghaec2-scaler-root"
    }
  }

  tags = {
    Name = "ghaec2-scaler"
    Type = "ghaec2-scaler"
  }

  lifecycle {
    create_before_destroy = true
  }
}

# Elastic IP for scaler (optional but recommended for stability)
resource "aws_eip" "scaler" {
  count    = var.create_elastic_ip ? 1 : 0
  instance = aws_instance.scaler.id
  domain   = "vpc"

  tags = {
    Name = "ghaec2-scaler-eip"
    Type = "ghaec2-scaler"
  }
} 