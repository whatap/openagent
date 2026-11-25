package rdsevent

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
)

// These tests run the real SDK client (signing, retries, XML parsing) against
// a local stand-in for the RDS and STS query APIs.

type awsRequest struct {
	form url.Values
	auth string
}

type awsStub struct {
	mu       sync.Mutex
	requests []awsRequest
	handle   func(n int, form url.Values, auth string) (int, string)
}

func (s *awsStub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	form, _ := url.ParseQuery(string(body))
	s.mu.Lock()
	s.requests = append(s.requests, awsRequest{form: form, auth: r.Header.Get("Authorization")})
	n := len(s.requests)
	s.mu.Unlock()
	status, resp := s.handle(n, form, r.Header.Get("Authorization"))
	w.Header().Set("Content-Type", "text/xml")
	w.WriteHeader(status)
	io.WriteString(w, resp)
}

func (s *awsStub) actions() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for _, r := range s.requests {
		out = append(out, r.form.Get("Action"))
	}
	return out
}

// isolateAWSEnv keeps the developer's AWS profile, keys and IMDS out of the test.
func isolateAWSEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{"AWS_PROFILE", "AWS_DEFAULT_PROFILE", "AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY",
		"AWS_SESSION_TOKEN", "AWS_REGION", "AWS_DEFAULT_REGION", "AWS_ROLE_ARN", "AWS_WEB_IDENTITY_TOKEN_FILE",
		"AWS_ENDPOINT_URL", "AWS_ENDPOINT_URL_RDS", "AWS_ENDPOINT_URL_STS", "AWS_MAX_ATTEMPTS", "AWS_RETRY_MODE"} {
		t.Setenv(key, "")
	}
	dir := t.TempDir()
	t.Setenv("AWS_CONFIG_FILE", filepath.Join(dir, "config"))
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(dir, "credentials"))
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
}

func stubFactory(endpoint string) *awsClientFactory {
	return &awsClientFactory{loadOptions: []func(*awsconfig.LoadOptions) error{
		awsconfig.WithBaseEndpoint(endpoint),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("AKIDBASECHAIN", "base-secret", "")),
	}}
}

const describeEventsPage1 = `<DescribeEventsResponse xmlns="http://rds.amazonaws.com/doc/2014-10-31/">
  <DescribeEventsResult>
    <Marker>page-2</Marker>
    <Events>
      <Event>
        <Message>Multi-AZ instance failover started.</Message>
        <SourceType>db-instance</SourceType>
        <EventCategories>
          <EventCategory>failover</EventCategory>
          <EventCategory>availability</EventCategory>
        </EventCategories>
        <Date>2026-11-25T02:58:01.123Z</Date>
        <SourceIdentifier>orders-db</SourceIdentifier>
        <SourceArn>arn:aws:rds:ap-northeast-2:111122223333:db:orders-db</SourceArn>
      </Event>
    </Events>
  </DescribeEventsResult>
  <ResponseMetadata><RequestId>req-1</RequestId></ResponseMetadata>
</DescribeEventsResponse>`

const describeEventsPage2 = `<DescribeEventsResponse xmlns="http://rds.amazonaws.com/doc/2014-10-31/">
  <DescribeEventsResult>
    <Events>
      <Event>
        <Message>DB instance restarted</Message>
        <SourceType>db-instance</SourceType>
        <EventCategories><EventCategory>availability</EventCategory></EventCategories>
        <Date>2026-11-25T02:59:30.000Z</Date>
        <SourceIdentifier>orders-db</SourceIdentifier>
        <SourceArn>arn:aws:rds:ap-northeast-2:111122223333:db:orders-db</SourceArn>
      </Event>
    </Events>
  </DescribeEventsResult>
  <ResponseMetadata><RequestId>req-2</RequestId></ResponseMetadata>
</DescribeEventsResponse>`

const emptyEvents = `<DescribeEventsResponse xmlns="http://rds.amazonaws.com/doc/2014-10-31/">
  <DescribeEventsResult><Events/></DescribeEventsResult>
  <ResponseMetadata><RequestId>req-e</RequestId></ResponseMetadata>
</DescribeEventsResponse>`

func queryError(code string) string {
	return fmt.Sprintf(`<ErrorResponse xmlns="http://rds.amazonaws.com/doc/2014-10-31/">
  <Error><Type>Sender</Type><Code>%s</Code><Message>%s from the test</Message></Error>
  <RequestId>req-err</RequestId>
</ErrorResponse>`, code, code)
}

func TestSDKClientRetriesThrottlingAndFollowsMarkers(t *testing.T) {
	isolateAWSEnv(t)
	stub := &awsStub{handle: func(n int, form url.Values, _ string) (int, string) {
		switch n {
		case 1:
			return http.StatusBadRequest, queryError("Throttling")
		case 2:
			return http.StatusOK, describeEventsPage1
		default:
			return http.StatusOK, describeEventsPage2
		}
	}}
	server := httptest.NewServer(stub)
	defer server.Close()

	cfg := testConfig(t, "ap-northeast-2")
	api, err := stubFactory(server.URL).Client(context.Background(), cfg, "ap-northeast-2")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 11, 25, 2, 50, 0, 0, time.UTC)
	end := time.Date(2026, 11, 25, 3, 0, 0, 0, time.UTC)
	events, pages, err := fetchWindow(context.Background(), api, start, end)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || pages != 2 {
		t.Fatalf("%d events in %d pages", len(events), pages)
	}
	if got := stub.actions(); !reflect.DeepEqual(got, []string{"DescribeEvents", "DescribeEvents", "DescribeEvents"}) {
		t.Fatalf("requests %v, want the throttled call retried", got)
	}
	first, third := stub.requests[0].form, stub.requests[2].form
	if first.Get("MaxRecords") != "100" || first.Get("StartTime") != "2026-11-25T02:50:00Z" || first.Get("EndTime") != "2026-11-25T03:00:00Z" || first.Get("Marker") != "" {
		t.Fatalf("first request %v", first)
	}
	if third.Get("Marker") != "page-2" {
		t.Fatalf("second page requested with marker %q", third.Get("Marker"))
	}
	if !strings.Contains(stub.requests[0].auth, "Credential=AKIDBASECHAIN/") || !strings.Contains(stub.requests[0].auth, "/ap-northeast-2/rds/aws4_request") {
		t.Fatalf("request not signed for rds in the region: %q", stub.requests[0].auth)
	}

	p, ok := EventToPack(events[0], "openagent")
	if !ok {
		t.Fatal("parsed event has no date")
	}
	want := []kv{
		{"category0", "failover"},
		{"category1", "availability"},
		{"sourceArn", "arn:aws:rds:ap-northeast-2:111122223333:db:orders-db"},
		{"sourceIdentifier", "orders-db"},
		{"sourceType", "db-instance"},
		{"agency", "openagent"},
	}
	if got := orderedText(t, p.Tags); !reflect.DeepEqual(got, want) || p.Time != 1795575481123 {
		t.Fatalf("pack from parsed XML: time %d tags %v", p.Time, got)
	}
	if p.Data.GetString("message") != "Multi-AZ instance failover started." {
		t.Fatalf("message %q", p.Data.GetString("message"))
	}
}

func TestSDKAssumeRoleSendsSessionNameAndExternalID(t *testing.T) {
	isolateAWSEnv(t)
	stub := &awsStub{handle: func(_ int, form url.Values, auth string) (int, string) {
		if form.Get("Action") == "AssumeRole" {
			return http.StatusOK, `<AssumeRoleResponse xmlns="https://sts.amazonaws.com/doc/2011-06-15/">
  <AssumeRoleResult>
    <Credentials>
      <AccessKeyId>ASIAASSUMEDROLE</AccessKeyId>
      <SecretAccessKey>assumed-secret</SecretAccessKey>
      <SessionToken>assumed-token</SessionToken>
      <Expiration>2099-01-01T00:00:00Z</Expiration>
    </Credentials>
    <AssumedRoleUser>
      <AssumedRoleId>AROATEST:whatap-openagent-rds-events</AssumedRoleId>
      <Arn>arn:aws:sts::111122223333:assumed-role/WhatapRdsEvents/whatap-openagent-rds-events</Arn>
    </AssumedRoleUser>
  </AssumeRoleResult>
  <ResponseMetadata><RequestId>req-sts</RequestId></ResponseMetadata>
</AssumeRoleResponse>`
		}
		return http.StatusOK, emptyEvents
	}}
	server := httptest.NewServer(stub)
	defer server.Close()

	cfg := testConfig(t, "ap-northeast-2")
	cfg.RoleARN = "arn:aws:iam::111122223333:role/WhatapRdsEvents"
	cfg.ExternalID = "example-external-id"
	api, err := stubFactory(server.URL).Client(context.Background(), cfg, "ap-northeast-2")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := fetchWindow(context.Background(), api, t0.Add(-time.Minute), t0); err != nil {
		t.Fatal(err)
	}
	if got := stub.actions(); !reflect.DeepEqual(got, []string{"AssumeRole", "DescribeEvents"}) {
		t.Fatalf("requests %v", got)
	}
	assume := stub.requests[0]
	if assume.form.Get("RoleArn") != cfg.RoleARN || assume.form.Get("RoleSessionName") != SessionName || assume.form.Get("ExternalId") != "example-external-id" {
		t.Fatalf("AssumeRole form %v", assume.form)
	}
	if !strings.Contains(assume.auth, "Credential=AKIDBASECHAIN/") {
		t.Fatalf("AssumeRole must be signed by the default chain: %q", assume.auth)
	}
	if describe := stub.requests[1]; !strings.Contains(describe.auth, "Credential=ASIAASSUMEDROLE/") {
		t.Fatalf("DescribeEvents not signed with the assumed role: %q", describe.auth)
	}
}

func TestSDKAccessDeniedIsReportedWithoutRetry(t *testing.T) {
	isolateAWSEnv(t)
	stub := &awsStub{handle: func(int, url.Values, string) (int, string) {
		return http.StatusForbidden, queryError("AccessDenied")
	}}
	server := httptest.NewServer(stub)
	defer server.Close()

	cfg := testConfig(t, "ap-northeast-2")
	api, err := stubFactory(server.URL).Client(context.Background(), cfg, "ap-northeast-2")
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = fetchWindow(context.Background(), api, t0.Add(-time.Minute), t0)
	if classify(err) != classAccessDenied {
		t.Fatalf("classify(%v) = %s", err, classify(err))
	}
	if n := len(stub.actions()); n != 1 {
		t.Fatalf("AccessDenied retried: %d requests", n)
	}
}

func TestFactoryReusesClientsAndRebuildsForAnotherRole(t *testing.T) {
	isolateAWSEnv(t)
	f := stubFactory("http://127.0.0.1:1")
	cfg := testConfig(t, "ap-northeast-2", "us-east-1")
	a1, _ := f.Client(context.Background(), cfg, "ap-northeast-2")
	a2, _ := f.Client(context.Background(), cfg, "ap-northeast-2")
	b, _ := f.Client(context.Background(), cfg, "us-east-1")
	if a1 != a2 || a1 == b {
		t.Fatal("want one cached client per region")
	}
	cfg.RoleARN = "arn:aws:iam::111122223333:role/Other"
	c, _ := f.Client(context.Background(), cfg, "ap-northeast-2")
	if c == a1 {
		t.Fatal("a new role must get new clients")
	}
	f.InvalidateCredentials(cfg) // must not panic for either credential type
	if _, ok := f.base.Credentials.(*aws.CredentialsCache); !ok {
		t.Fatalf("assumed-role credentials are not cached: %T", f.base.Credentials)
	}
}
