package rdsevent

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/ratelimit"
	"github.com/aws/aws-sdk-go-v2/aws/retry"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/aws/smithy-go"
)

// SessionName is the RoleSessionName used when roleArn is set. It shows in
// the customer's CloudTrail as the caller of rds:DescribeEvents.
const SessionName = "whatap-openagent-rds-events"

const (
	// SDK-level retries handle throttling with exponential backoff and
	// jitter before an error reaches the collector.
	sdkMaxAttempts = 5
	sdkMaxBackoff  = 20 * time.Second
)

// EventsAPI is the part of the RDS client the collector uses. Tests replace it.
type EventsAPI interface {
	DescribeEvents(ctx context.Context, params *rds.DescribeEventsInput, optFns ...func(*rds.Options)) (*rds.DescribeEventsOutput, error)
}

// ClientFactory hands out one RDS client per region for the credentials in cfg.
type ClientFactory interface {
	Client(ctx context.Context, cfg Config, region string) (EventsAPI, error)
	// InvalidateCredentials drops cached credentials, for example after an
	// expired-token error, so the next call fetches new ones.
	InvalidateCredentials(cfg Config)
}

type credentialKey struct {
	roleARN    string
	externalID string
	stsRegion  string
}

// awsClientFactory uses the SDK default credential chain (environment, shared
// files, ECS/EKS task roles, EC2 instance role). With roleArn it assumes that
// role on top of the chain; aws.CredentialsCache renews the session before it
// expires.
type awsClientFactory struct {
	mu      sync.Mutex
	key     credentialKey
	base    *aws.Config
	clients map[string]*rds.Client

	// Extra options for tests (endpoint, HTTP client, static credentials).
	loadOptions []func(*awsconfig.LoadOptions) error
	rdsOptions  []func(*rds.Options)
}

// NewAWSClientFactory returns the factory used by the agent and the dry run.
func NewAWSClientFactory() ClientFactory {
	return &awsClientFactory{}
}

func (f *awsClientFactory) Client(ctx context.Context, cfg Config, region string) (EventsAPI, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := credentialKey{roleARN: cfg.RoleARN, externalID: cfg.ExternalID}
	if len(cfg.Regions) > 0 {
		key.stsRegion = cfg.Regions[0]
	}
	if f.base == nil || f.key != key {
		base, err := f.loadBaseConfig(ctx, cfg, key.stsRegion)
		if err != nil {
			return nil, err
		}
		f.base, f.key, f.clients = &base, key, make(map[string]*rds.Client)
	}
	if client, ok := f.clients[region]; ok {
		return client, nil
	}
	opts := append([]func(*rds.Options){func(o *rds.Options) { o.Region = region }}, f.rdsOptions...)
	client := rds.NewFromConfig(*f.base, opts...)
	f.clients[region] = client
	return client, nil
}

func (f *awsClientFactory) loadBaseConfig(ctx context.Context, cfg Config, stsRegion string) (aws.Config, error) {
	opts := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRetryer(newRetryer),
	}
	if stsRegion != "" {
		opts = append(opts, awsconfig.WithRegion(stsRegion))
	}
	opts = append(opts, f.loadOptions...)
	base, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return aws.Config{}, err
	}
	if cfg.RoleARN != "" {
		// The STS client signs AssumeRole with the default chain; the RDS
		// clients then use the assumed role.
		stsClient := sts.NewFromConfig(base)
		provider := stscreds.NewAssumeRoleProvider(stsClient, cfg.RoleARN, func(o *stscreds.AssumeRoleOptions) {
			o.RoleSessionName = SessionName
			if cfg.ExternalID != "" {
				o.ExternalID = aws.String(cfg.ExternalID)
			}
		})
		base.Credentials = aws.NewCredentialsCache(provider)
	}
	return base, nil
}

func (f *awsClientFactory) InvalidateCredentials(Config) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.base == nil {
		return
	}
	if cache, ok := f.base.Credentials.(*aws.CredentialsCache); ok {
		cache.Invalidate()
	}
}

// newRetryer retries throttling and transient errors with exponential backoff
// and jitter. The client-side retry quota is off: a poller this small should
// not fail fast because earlier retries spent the token bucket.
func newRetryer() aws.Retryer {
	return retry.NewStandard(func(o *retry.StandardOptions) {
		o.MaxAttempts = sdkMaxAttempts
		o.MaxBackoff = sdkMaxBackoff
		o.RateLimiter = ratelimit.None
	})
}

// errorClass groups AWS errors by what the collector does about them.
type errorClass int

const (
	classOther errorClass = iota
	classAccessDenied
	classCredentials
	classThrottled
	classTimeout
)

func (c errorClass) String() string {
	switch c {
	case classAccessDenied:
		return "access-denied"
	case classCredentials:
		return "credentials"
	case classThrottled:
		return "throttled"
	case classTimeout:
		return "timeout"
	}
	return "error"
}

// hint is the operator's next step for the class, written to the log.
func (c errorClass) hint() string {
	switch c {
	case classAccessDenied:
		return "check rds:DescribeEvents on the role and, with roleArn, sts:AssumeRole plus the role's trust policy (and externalId)"
	case classCredentials:
		return "credentials are missing, invalid or expired; cached credentials were dropped and are fetched again next round"
	case classThrottled:
		return "AWS throttled DescribeEvents after SDK retries; this region waits longer before the next try"
	case classTimeout:
		return "the round timed out; it is retried next round"
	}
	return "retried next round"
}

func classify(err error) errorClass {
	if err == nil {
		return classOther
	}
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.ErrorCode() {
		case "AccessDenied", "AccessDeniedException", "UnauthorizedOperation", "AuthorizationError",
			"NotAuthorized", "UnauthorizedAccess":
			return classAccessDenied
		case "ExpiredToken", "ExpiredTokenException", "RequestExpired", "InvalidClientTokenId",
			"UnrecognizedClientException", "SignatureDoesNotMatch", "InvalidAccessKeyId",
			"IncompleteSignature", "MissingAuthenticationToken", "InvalidToken", "TokenRefreshRequired":
			return classCredentials
		case "Throttling", "ThrottlingException", "ThrottledException", "RequestLimitExceeded",
			"TooManyRequestsException", "RequestThrottled", "RequestThrottledException",
			"SlowDown", "PriorRequestNotComplete", "BandwidthLimitExceeded":
			return classThrottled
		}
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return classTimeout
	}
	// The credential chain reports a missing EC2 instance role or a failed
	// refresh without an AWS error code.
	msg := err.Error()
	for _, marker := range []string{"failed to retrieve credentials", "failed to refresh cached credentials", "no EC2 IMDS role found", "anonymous credentials"} {
		if strings.Contains(msg, marker) {
			return classCredentials
		}
	}
	return classOther
}
