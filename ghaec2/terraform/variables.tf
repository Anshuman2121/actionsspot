# AWS Configuration
variable "aws_region" {
  description = "AWS region"
  type        = string
  default     = "eu-north-1"
}

variable "vpc_id" {
  description = "VPC ID to deploy resources in (leave empty to use default VPC)"
  type        = string
  default     = ""
}

variable "subnet_id" {
  description = "Subnet ID to deploy scaler instance in (leave empty to use first available public subnet)"
  type        = string
  default     = ""
}

# SSH access removed - using SSM Session Manager instead

# Key Pair Configuration
variable "key_pair_name" {
  description = "Name of existing EC2 key pair (leave empty to create new one)"
  type        = string
  default     = ""
}

variable "public_key" {
  description = "Public key content (required if key_pair_name is empty)"
  type        = string
  default     = ""
}

# GitHub Configuration
variable "github_token" {
  description = "GitHub personal access token with admin:org permissions"
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

# Runner Configuration
variable "runner_labels" {
  description = "Labels for GitHub Actions runners"
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

variable "runner_scale_set_name" {
  description = "Name for the runner scale set"
  type        = string
  default     = "ghaec2-scaler"
}

# EC2 Configuration
variable "scaler_instance_type" {
  description = "Instance type for the scaler instance"
  type        = string
  default     = "t3.medium"
}

variable "runner_instance_type" {
  description = "Instance type for runner instances"
  type        = string
  default     = "t3.medium"
}

variable "spot_price" {
  description = "Maximum spot price for runner instances"
  type        = string
  default     = "0.05"
}

variable "create_elastic_ip" {
  description = "Whether to create an Elastic IP for the scaler instance"
  type        = bool
  default     = true
} 