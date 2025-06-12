# Instance Information
output "scaler_instance_id" {
  description = "ID of the scaler EC2 instance"
  value       = aws_instance.scaler.id
}

output "scaler_public_ip" {
  description = "Public IP address of the scaler instance"
  value       = var.create_elastic_ip ? aws_eip.scaler[0].public_ip : aws_instance.scaler.public_ip
}

output "scaler_private_ip" {
  description = "Private IP address of the scaler instance"
  value       = aws_instance.scaler.private_ip
}

# Network Information
output "vpc_id" {
  description = "ID of the VPC used"
  value       = local.vpc_id
}

output "subnet_id" {
  description = "ID of the subnet used"
  value       = local.subnet_id
}

output "scaler_security_group_id" {
  description = "ID of the scaler security group"
  value       = aws_security_group.scaler.id
}

output "runners_security_group_id" {
  description = "ID of the runners security group"
  value       = aws_security_group.runners.id
}

# IAM Information
output "scaler_role_arn" {
  description = "ARN of the scaler IAM role"
  value       = aws_iam_role.scaler_role.arn
}

output "scaler_instance_profile_name" {
  description = "Name of the scaler instance profile"
  value       = aws_iam_instance_profile.scaler_profile.name
}

# Key Pair Information
output "key_pair_name" {
  description = "Name of the key pair used"
  value       = local.key_pair_name
}

# AMI Information
output "ubuntu_ami_id" {
  description = "ID of the Ubuntu AMI used"
  value       = data.aws_ami.ubuntu.id
}

# SSH Connection
output "ssh_command" {
  description = "SSH command to connect to the scaler instance"
  value       = "ssh -i ${local.key_pair_name}.pem ubuntu@${var.create_elastic_ip ? aws_eip.scaler[0].public_ip : aws_instance.scaler.public_ip}"
}

# Environment Variables for .env file
output "environment_variables" {
  description = "Environment variables for the .env file"
  value = {
    GITHUB_TOKEN           = "SENSITIVE - Set manually"
    GITHUB_ENTERPRISE_URL  = var.github_enterprise_url
    ORGANIZATION_NAME      = var.organization_name
    RUNNER_LABELS          = join(",", var.runner_labels)
    MIN_RUNNERS           = var.min_runners
    MAX_RUNNERS           = var.max_runners
    RUNNER_SCALE_SET_NAME = var.runner_scale_set_name
    AWS_REGION            = var.aws_region
    EC2_SUBNET_ID         = local.subnet_id
    EC2_SECURITY_GROUP_ID = aws_security_group.runners.id
    EC2_KEY_PAIR_NAME     = local.key_pair_name
    EC2_INSTANCE_TYPE     = var.runner_instance_type
    EC2_AMI_ID            = data.aws_ami.ubuntu.id
    EC2_SPOT_PRICE        = var.spot_price
  }
} 