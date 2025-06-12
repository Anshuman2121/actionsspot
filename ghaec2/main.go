package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/go-logr/zapr"
	"go.uber.org/zap"
)

// Configuration from environment variables
type Config struct {
	// GitHub Configuration
	GitHubToken        string
	GitHubEnterpriseURL string
	OrganizationName   string
	RunnerLabels       []string
	
	// Runner Scale Set Configuration
	RunnerScaleSetID   int
	RunnerScaleSetName string
	MinRunners         int
	MaxRunners         int
	
	// AWS Configuration
	AWSRegion           string
	EC2SubnetID         string
	EC2SecurityGroupID  string
	EC2KeyPairName      string
	EC2InstanceType     string
	EC2AMI              string
	EC2SpotPrice        string
}

// LoadConfig loads configuration from environment variables
func LoadConfig() (*Config, error) {
	config := &Config{
		GitHubToken:        os.Getenv("GITHUB_TOKEN"),
		GitHubEnterpriseURL: strings.TrimSuffix(os.Getenv("GITHUB_ENTERPRISE_URL"), "/"),
		OrganizationName:   os.Getenv("ORGANIZATION_NAME"),
		RunnerScaleSetName: os.Getenv("RUNNER_SCALE_SET_NAME"),
		AWSRegion:          os.Getenv("AWS_REGION"),
		EC2SubnetID:        os.Getenv("EC2_SUBNET_ID"),
		EC2SecurityGroupID: os.Getenv("EC2_SECURITY_GROUP_ID"),
		EC2KeyPairName:     os.Getenv("EC2_KEY_PAIR_NAME"),
		EC2InstanceType:    os.Getenv("EC2_INSTANCE_TYPE"),
		EC2AMI:             os.Getenv("EC2_AMI_ID"),
		EC2SpotPrice:       os.Getenv("EC2_SPOT_PRICE"),
	}
	
	// Parse runner labels
	if labels := os.Getenv("RUNNER_LABELS"); labels != "" {
		config.RunnerLabels = strings.Split(labels, ",")
		for i, label := range config.RunnerLabels {
			config.RunnerLabels[i] = strings.TrimSpace(label)
		}
	} else {
		config.RunnerLabels = []string{"self-hosted", "linux", "x64", "ghalistener-managed"}
	}
	
	// Parse integer values
	var err error
	if scaleSetID := os.Getenv("RUNNER_SCALE_SET_ID"); scaleSetID != "" {
		config.RunnerScaleSetID, err = strconv.Atoi(scaleSetID)
		if err != nil {
			return nil, fmt.Errorf("invalid RUNNER_SCALE_SET_ID: %w", err)
		}
	}
	
	if minRunners := os.Getenv("MIN_RUNNERS"); minRunners != "" {
		config.MinRunners, err = strconv.Atoi(minRunners)
		if err != nil {
			return nil, fmt.Errorf("invalid MIN_RUNNERS: %w", err)
		}
	}
	
	if maxRunners := os.Getenv("MAX_RUNNERS"); maxRunners != "" {
		config.MaxRunners, err = strconv.Atoi(maxRunners)
		if err != nil {
			return nil, fmt.Errorf("invalid MAX_RUNNERS: %w", err)
		}
	} else {
		config.MaxRunners = 10 // Default
	}
	
	// Set defaults
	if config.EC2InstanceType == "" {
		config.EC2InstanceType = "t3.medium"
	}
	if config.EC2SpotPrice == "" {
		config.EC2SpotPrice = "0.05"
	}
	if config.AWSRegion == "" {
		config.AWSRegion = "eu-north-1"
	}
	if config.RunnerScaleSetName == "" {
		config.RunnerScaleSetName = "ghaec2-scaler"
	}
	
	return config, nil
}

// Validate checks if all required configuration is present
func (c *Config) Validate() error {
	required := map[string]string{
		"GITHUB_TOKEN":           c.GitHubToken,
		"GITHUB_ENTERPRISE_URL":  c.GitHubEnterpriseURL,
		"ORGANIZATION_NAME":      c.OrganizationName,
		"EC2_SUBNET_ID":          c.EC2SubnetID,
		"EC2_SECURITY_GROUP_ID":  c.EC2SecurityGroupID,
		"EC2_KEY_PAIR_NAME":      c.EC2KeyPairName,
		"EC2_AMI_ID":             c.EC2AMI,
	}
	
	for name, value := range required {
		if value == "" {
			return fmt.Errorf("required environment variable %s is not set", name)
		}
	}

	// Validate GitHub token format
	if !strings.HasPrefix(c.GitHubToken, "ghp_") && !strings.HasPrefix(c.GitHubToken, "ghs_") {
		return fmt.Errorf("GITHUB_TOKEN must start with 'ghp_' (personal access token) or 'ghs_' (GitHub App token)")
	}

	// Validate GitHub Enterprise URL format
	if !strings.HasPrefix(c.GitHubEnterpriseURL, "https://") {
		return fmt.Errorf("GITHUB_ENTERPRISE_URL must start with 'https://'")
	}

	// Remove any trailing slashes from GitHub Enterprise URL
	c.GitHubEnterpriseURL = strings.TrimSuffix(c.GitHubEnterpriseURL, "/")

	// Ensure URL doesn't contain /api/v3 as it will be added by the client
	c.GitHubEnterpriseURL = strings.TrimSuffix(c.GitHubEnterpriseURL, "/api/v3")
	
	if c.MaxRunners <= 0 {
		return fmt.Errorf("MAX_RUNNERS must be > 0")
	}
	
	if c.MinRunners < 0 {
		return fmt.Errorf("MIN_RUNNERS must be >= 0")
	}
	
	if c.MinRunners > c.MaxRunners {
		return fmt.Errorf("MIN_RUNNERS (%d) cannot be greater than MAX_RUNNERS (%d)", c.MinRunners, c.MaxRunners)
	}
	
	return nil
}

func main() {
	// Initialize logger
	zapLogger, err := zap.NewProduction()
	if err != nil {
		log.Fatalf("Failed to create logger: %v", err)
	}
	defer zapLogger.Sync()
	
	logger := zapr.NewLogger(zapLogger)
	
	// Load configuration
	cfg, err := LoadConfig()
	if err != nil {
		logger.Error(err, "Failed to load configuration")
		os.Exit(1)
	}
	
	if err := cfg.Validate(); err != nil {
		logger.Error(err, "Configuration validation failed")
		os.Exit(1)
	}
	
	logger.Info("Starting GitHub Actions Listener EC2 Scaler",
		"scaleSetID", cfg.RunnerScaleSetID,
		"scaleSetName", cfg.RunnerScaleSetName,
		"organization", cfg.OrganizationName,
		"minRunners", cfg.MinRunners,
		"maxRunners", cfg.MaxRunners,
		"runnerLabels", cfg.RunnerLabels,
	)
	
	// Initialize AWS clients
	ctx := context.Background()
	awsConfig, err := config.LoadDefaultConfig(ctx, config.WithRegion(cfg.AWSRegion))
	if err != nil {
		logger.Error(err, "Failed to load AWS configuration")
		os.Exit(1)
	}
	
	ec2Client := ec2.NewFromConfig(awsConfig)
	
	// Create the scaler service
	scaler, err := NewGHAListenerScaler(ctx, cfg, ec2Client, logger)
	if err != nil {
		logger.Error(err, "Failed to create scaler service")
		os.Exit(1)
	}
	
	// Setup graceful shutdown
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	
	// Handle shutdown signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	
	go func() {
		sig := <-sigChan
		logger.Info("Received shutdown signal", "signal", sig)
		cancel()
	}()
	
	// Start the scaler
	logger.Info("Starting GitHub Actions Listener Scaler")
	if err := scaler.Run(ctx); err != nil {
		logger.Error(err, "Scaler failed")
		os.Exit(1)
	}
	
	logger.Info("GitHub Actions Listener Scaler stopped")
} 