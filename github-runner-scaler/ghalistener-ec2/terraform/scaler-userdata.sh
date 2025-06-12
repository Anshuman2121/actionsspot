#!/bin/bash
set -e

# Log everything to user-data.log
exec > >(tee /var/log/user-data.log|logger -t user-data -s 2>/dev/console) 2>&1

echo "Starting GHA Listener Scaler EC2 setup..."

# Update system
apt-get update
apt-get install -y wget curl unzip awscli golang-go git supervisor jq

# Install CloudWatch agent
wget https://s3.amazonaws.com/amazoncloudwatch-agent/ubuntu/amd64/latest/amazon-cloudwatch-agent.deb
dpkg -i amazon-cloudwatch-agent.deb

# Create application directory
mkdir -p /opt/gha-listener-scaler
cd /opt/gha-listener-scaler

# Create environment file
cat > /opt/gha-listener-scaler/.env << 'EOF'
# GitHub Configuration
GITHUB_TOKEN=${github_token}
GITHUB_ENTERPRISE_URL=${github_enterprise_url}
ORGANIZATION_NAME=${organization_name}
RUNNER_SCALE_SET_NAME=${runner_scale_set_name}
RUNNER_LABELS=${runner_labels}

# Runner Scale Set Configuration (will be populated after first run)
RUNNER_SCALE_SET_ID=
MIN_RUNNERS=${min_runners}
MAX_RUNNERS=${max_runners}

# AWS Configuration
AWS_REGION=${aws_region}
EC2_SUBNET_ID=${ec2_subnet_id}
EC2_SECURITY_GROUP_ID=${ec2_security_group_id}
EC2_KEY_PAIR_NAME=${ec2_key_pair_name}
EC2_INSTANCE_TYPE=${ec2_instance_type}
EC2_AMI_ID=${ec2_ami_id}
EC2_SPOT_PRICE=${ec2_spot_price}
RUNNER_INSTANCE_PROFILE=${runner_instance_profile}
EOF

# Create Go module
cat > go.mod << 'EOF'
module gha-listener-scaler

go 1.21

require (
    github.com/aws/aws-sdk-go-v2 v1.21.0
    github.com/aws/aws-sdk-go-v2/config v1.18.45
    github.com/aws/aws-sdk-go-v2/service/dynamodb v1.21.5
    github.com/aws/aws-sdk-go-v2/service/ec2 v1.118.0
    github.com/go-logr/logr v1.2.4
    github.com/go-logr/zapr v1.2.4
    github.com/google/uuid v1.3.1
    go.uber.org/zap v1.25.0
)
EOF

# Download source code from the terraform directory
# In a real deployment, you'd want to package this as a binary or use a Git repository

# Create a simple download script that gets the source
cat > download-source.sh << 'EOF'
#!/bin/bash
# This is a placeholder - in production you'd download from Git or S3
echo "Source code should be deployed here"
# For now, create minimal placeholder files
touch main.go gha_actions_client.go scaler.go
EOF

chmod +x download-source.sh
./download-source.sh

# Create systemd service
cat > /etc/systemd/system/gha-listener-scaler.service << 'EOF'
[Unit]
Description=GitHub Actions Listener Scaler
After=network.target
Wants=network.target

[Service]
Type=simple
User=root
WorkingDirectory=/opt/gha-listener-scaler
EnvironmentFile=/opt/gha-listener-scaler/.env
ExecStart=/opt/gha-listener-scaler/gha-listener-scaler
Restart=always
RestartSec=10
StandardOutput=journal
StandardError=journal
SyslogIdentifier=gha-listener-scaler

[Install]
WantedBy=multi-user.target
EOF

# Create CloudWatch agent configuration
cat > /opt/aws/amazon-cloudwatch-agent/etc/amazon-cloudwatch-agent.json << 'EOF'
{
    "logs": {
        "logs_collected": {
            "files": {
                "collect_list": [
                    {
                        "file_path": "/var/log/user-data.log",
                        "log_group_name": "/aws/ec2/gha-listener-scaler",
                        "log_stream_name": "{instance_id}/user-data.log"
                    },
                    {
                        "file_path": "/var/log/syslog",
                        "log_group_name": "/aws/ec2/gha-listener-scaler",
                        "log_stream_name": "{instance_id}/syslog"
                    }
                ]
            }
        }
    },
    "metrics": {
        "namespace": "GHA/ListenerScaler",
        "metrics_collected": {
            "cpu": {
                "measurement": [
                    "cpu_usage_idle",
                    "cpu_usage_iowait",
                    "cpu_usage_user",
                    "cpu_usage_system"
                ],
                "metrics_collection_interval": 60
            },
            "disk": {
                "measurement": [
                    "used_percent"
                ],
                "metrics_collection_interval": 60,
                "resources": [
                    "*"
                ]
            },
            "mem": {
                "measurement": [
                    "mem_used_percent"
                ],
                "metrics_collection_interval": 60
            }
        }
    }
}
EOF

# Start CloudWatch agent
/opt/aws/amazon-cloudwatch-agent/bin/amazon-cloudwatch-agent-ctl \
    -a fetch-config \
    -m ec2 \
    -c file:/opt/aws/amazon-cloudwatch-agent/etc/amazon-cloudwatch-agent.json \
    -s

# Create a startup script that builds and runs the application
cat > /opt/gha-listener-scaler/start.sh << 'EOF'
#!/bin/bash
set -e

cd /opt/gha-listener-scaler

# Source environment variables
source .env

# Build the application
echo "Building GHA Listener Scaler..."
go mod tidy
go build -o gha-listener-scaler .

# Start the service
echo "Starting GHA Listener Scaler service..."
systemctl enable gha-listener-scaler
systemctl start gha-listener-scaler

echo "GHA Listener Scaler started successfully"
EOF

chmod +x /opt/gha-listener-scaler/start.sh

# Create a health check script
cat > /opt/gha-listener-scaler/health-check.sh << 'EOF'
#!/bin/bash
# Check if the service is running
if systemctl is-active --quiet gha-listener-scaler; then
    echo "✅ GHA Listener Scaler is running"
    exit 0
else
    echo "❌ GHA Listener Scaler is not running"
    systemctl status gha-listener-scaler
    exit 1
fi
EOF

chmod +x /opt/gha-listener-scaler/health-check.sh

# Set up log rotation
cat > /etc/logrotate.d/gha-listener-scaler << 'EOF'
/var/log/gha-listener-scaler/*.log {
    daily
    missingok
    rotate 7
    compress
    delaycompress
    copytruncate
    notifempty
}
EOF

# Create monitoring script
cat > /opt/gha-listener-scaler/monitor.sh << 'EOF'
#!/bin/bash
# Simple monitoring script that restarts the service if it fails

while true; do
    if ! /opt/gha-listener-scaler/health-check.sh > /dev/null 2>&1; then
        echo "$(date): Service unhealthy, restarting..."
        systemctl restart gha-listener-scaler
        sleep 30
    fi
    sleep 60
done
EOF

chmod +x /opt/gha-listener-scaler/monitor.sh

# Start the monitoring script in the background
nohup /opt/gha-listener-scaler/monitor.sh > /var/log/gha-listener-monitor.log 2>&1 &

# NOTE: In a production deployment, you would:
# 1. Download pre-built binaries from S3 or GitHub releases
# 2. Use proper secrets management (AWS Secrets Manager)
# 3. Set up proper monitoring and alerting
# 4. Use a configuration management tool like Ansible

echo "✅ GHA Listener Scaler EC2 setup completed"
echo "📋 Next steps:"
echo "   1. SSH to the instance: ssh -i your-key.pem ubuntu@$(curl -s http://169.254.169.254/latest/meta-data/public-ipv4)"
echo "   2. Check logs: sudo journalctl -u gha-listener-scaler -f"
echo "   3. Check health: sudo /opt/gha-listener-scaler/health-check.sh"

# Signal that user data script completed
/opt/aws/bin/cfn-signal -e $? --stack=${AWS::StackName} --resource=ScalerInstance --region=${AWS::Region} || true 