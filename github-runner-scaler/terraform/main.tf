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

# Variables
variable "aws_region" {
  description = "AWS region"
  type        = string
  default     = "us-east-1"
}

variable "github_token" {
  description = "GitHub Personal Access Token"
  type        = string
  sensitive   = true
}

variable "github_enterprise_url" {
  description = "GitHub Enterprise Server URL"
  type        = string
  default     = "https://TelenorSwedenAB.ghe.com"
}

variable "organization_name" {
  description = "GitHub Organization Name"
  type        = string
  default     = "TelenorSweden"
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
  description = "EC2 Key Pair name"
  type        = string
}

variable "runner_labels" {
  description = "Labels for GitHub runners"
  type        = list(string)
  default     = ["self-hosted", "linux", "x64", "lambda-managed"]
}

variable "cleanup_offline_runners" {
  description = "Automatically cleanup offline runners"
  type        = bool
  default     = true
}

# DynamoDB table for tracking runners
resource "aws_dynamodb_table" "github_runners" {
  name           = "github-runners"
  billing_mode   = "PAY_PER_REQUEST"
  hash_key       = "runner_id"

  attribute {
    name = "runner_id"
    type = "S"
  }

  attribute {
    name = "job_request_id"
    type = "N"
  }

  attribute {
    name = "status"
    type = "S"
  }

  global_secondary_index {
    name            = "JobRequestIndex"
    hash_key        = "job_request_id"
    projection_type = "ALL"
  }

  global_secondary_index {
    name            = "StatusIndex"
    hash_key        = "status"
    projection_type = "ALL"
  }

  tags = {
    Name = "GitHub Runners"
  }
}

# DynamoDB table for sessions
resource "aws_dynamodb_table" "github_sessions" {
  name           = "github-runners-sessions"
  billing_mode   = "PAY_PER_REQUEST"
  hash_key       = "session_id"

  attribute {
    name = "session_id"
    type = "S"
  }

  tags = {
    Name = "GitHub Runner Sessions"
  }
}

# Security group for EC2 instances
resource "aws_security_group" "github_runners" {
  name_prefix = "github-runners-"
  vpc_id      = data.aws_subnet.selected.vpc_id

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  ingress {
    from_port   = 22
    to_port     = 22
    protocol    = "tcp"
    cidr_blocks = ["10.0.0.0/8"]
  }

  tags = {
    Name = "GitHub Runners Security Group"
  }
}

data "aws_subnet" "selected" {
  id = var.ec2_subnet_id
}

# IAM role for Lambda
resource "aws_iam_role" "lambda_role" {
  name = "github-runner-scaler-lambda-role"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Action = "sts:AssumeRole"
        Effect = "Allow"
        Principal = {
          Service = "lambda.amazonaws.com"
        }
      }
    ]
  })
}

# IAM policy for Lambda
resource "aws_iam_policy" "lambda_policy" {
  name = "github-runner-scaler-lambda-policy"

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Action = [
          "logs:CreateLogGroup",
          "logs:CreateLogStream",
          "logs:PutLogEvents"
        ]
        Resource = "arn:aws:logs:*:*:*"
      },
      {
        Effect = "Allow"
        Action = [
          "dynamodb:PutItem",
          "dynamodb:GetItem",
          "dynamodb:UpdateItem",
          "dynamodb:DeleteItem",
          "dynamodb:Query",
          "dynamodb:Scan"
        ]
        Resource = [
          aws_dynamodb_table.github_runners.arn,
          aws_dynamodb_table.github_sessions.arn,
          "${aws_dynamodb_table.github_runners.arn}/index/*",
          "${aws_dynamodb_table.github_sessions.arn}/index/*"
        ]
      },
      {
        Effect = "Allow"
        Action = [
          "ec2:RequestSpotInstances",
          "ec2:DescribeSpotInstanceRequests",
          "ec2:DescribeInstances",
          "ec2:TerminateInstances",
          "ec2:CreateTags",
          "ec2:DescribeTags"
        ]
        Resource = "*"
      },
      {
        Effect = "Allow"
        Action = [
          "events:PutRule",
          "events:PutTargets",
          "events:DeleteRule",
          "events:RemoveTargets"
        ]
        Resource = "*"
      },
      {
        Effect = "Allow"
        Action = [
          "iam:PassRole"
        ]
        Resource = aws_iam_role.ec2_role.arn
      }
    ]
  })
}

resource "aws_iam_role_policy_attachment" "lambda_policy" {
  role       = aws_iam_role.lambda_role.name
  policy_arn = aws_iam_policy.lambda_policy.arn
}

# IAM role for EC2 instances
resource "aws_iam_role" "ec2_role" {
  name = "github-runner-ec2-role"

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

# IAM policy for EC2 instances
resource "aws_iam_policy" "ec2_policy" {
  name = "github-runner-ec2-policy"

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Action = [
          "logs:CreateLogGroup",
          "logs:CreateLogStream",
          "logs:PutLogEvents",
          "logs:DescribeLogGroups",
          "logs:DescribeLogStreams"
        ]
        Resource = "*"
      },
      {
        Effect = "Allow"
        Action = [
          "dynamodb:UpdateItem"
        ]
        Resource = aws_dynamodb_table.github_runners.arn
      },
      {
        Effect = "Allow"
        Action = [
          "ec2:TerminateInstances",
          "ec2:DescribeInstances"
        ]
        Resource = "*"
        Condition = {
          StringEquals = {
            "ec2:ResourceTag/ManagedBy" = "github-runner-scaler-lambda"
          }
        }
      }
    ]
  })
}

resource "aws_iam_role_policy_attachment" "ec2_policy" {
  role       = aws_iam_role.ec2_role.name
  policy_arn = aws_iam_policy.ec2_policy.arn
}

resource "aws_iam_instance_profile" "ec2_profile" {
  name = "github-runner-ec2-profile"
  role = aws_iam_role.ec2_role.name
}

# Lambda function
resource "aws_lambda_function" "github_runner_scaler" {
  filename         = "../github-runner-scaler.zip"
  function_name    = "github-runner-scaler"
  role            = aws_iam_role.lambda_role.arn
  handler         = "main"
  runtime         = "provided.al2"
  timeout         = 900 # 15 minutes

  environment {
    variables = {
      GITHUB_TOKEN                 = var.github_token
      GITHUB_ENTERPRISE_URL        = var.github_enterprise_url
      ORGANIZATION_NAME            = var.organization_name
      MIN_RUNNERS                  = var.min_runners
      MAX_RUNNERS                  = var.max_runners
      EC2_INSTANCE_TYPE            = var.ec2_instance_type
      EC2_AMI_ID                   = var.ec2_ami_id
      EC2_SUBNET_ID                = var.ec2_subnet_id
      EC2_SECURITY_GROUP_ID        = aws_security_group.github_runners.id
      EC2_KEY_PAIR_NAME            = var.ec2_key_pair_name
      EC2_SPOT_PRICE               = "0.05"
      DYNAMODB_TABLE_NAME          = aws_dynamodb_table.github_runners.name
      RUNNER_LABELS                = jsonencode(var.runner_labels)
      CLEANUP_OFFLINE_RUNNERS      = var.cleanup_offline_runners
    }
  }

  depends_on = [
    aws_iam_role_policy_attachment.lambda_policy,
    aws_cloudwatch_log_group.lambda_logs,
  ]
}

# CloudWatch Log Group
resource "aws_cloudwatch_log_group" "lambda_logs" {
  name              = "/aws/lambda/github-runner-scaler"
  retention_in_days = 14
}

# EventBridge rule to trigger Lambda every 60 seconds
resource "aws_cloudwatch_event_rule" "github_runner_scaler_schedule" {
  name                = "github-runner-scaler-schedule"
  description         = "Trigger GitHub Runner Scaler Lambda every 60 seconds"
  schedule_expression = "rate(1 minute)"
}

resource "aws_cloudwatch_event_target" "lambda_target" {
  rule      = aws_cloudwatch_event_rule.github_runner_scaler_schedule.name
  target_id = "GitHubRunnerScalerTarget"
  arn       = aws_lambda_function.github_runner_scaler.arn
}

resource "aws_lambda_permission" "allow_eventbridge" {
  statement_id  = "AllowExecutionFromEventBridge"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.github_runner_scaler.function_name
  principal     = "events.amazonaws.com"
  source_arn    = aws_cloudwatch_event_rule.github_runner_scaler_schedule.arn
}

# Outputs
output "lambda_function_arn" {
  description = "ARN of the Lambda function"
  value       = aws_lambda_function.github_runner_scaler.arn
}

output "dynamodb_table_name" {
  description = "Name of the DynamoDB table"
  value       = aws_dynamodb_table.github_runners.name
}

output "security_group_id" {
  description = "Security Group ID for runners"
  value       = aws_security_group.github_runners.id
} 