package substrate

import (
	"context"
	"errors"
	pb "github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"net"
	"regexp"
	"sort"
	"strings"
)

// EnsureRuntimeEgress installs one immutable, actor-scoped TLS allowlist.
// Existing policies must match exactly; retries never broaden permission.
func (c *Client) EnsureRuntimeEgress(ctx context.Context, space, actor string, hosts []string) error {
	if len(hosts) == 0 || len(hosts) > 16 {
		return errors.New("invalid runtime allowlist")
	}
	names := append([]string(nil), hosts...)
	pattern := regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]*[a-z0-9])?)+$`)
	for _, h := range names {
		if !pattern.MatchString(h) || net.ParseIP(h) != nil || strings.HasSuffix(h, ".svc") || strings.HasSuffix(h, ".local") || strings.HasSuffix(h, ".cluster.local") {
			return errors.New("invalid runtime hostname")
		}
	}
	sort.Strings(names)
	for i := 1; i < len(names); i++ {
		if names[i] == names[i-1] {
			return errors.New("duplicate runtime hostname")
		}
	}
	want := &pb.EgressPolicy{Metadata: &pb.ResourceMetadata{Name: "default", Atespace: space}, Rules: []*pb.EgressRule{{TlsPassthrough: &pb.TLSPassthroughRule{Hostnames: names, Ports: &pb.Ports{Numbers: []int32{443}}}}}}
	ref := &pb.ObjectRef{Atespace: space, Name: actor}
	got, e := c.control.GetActorEgressPolicy(ctx, &pb.GetActorEgressPolicyRequest{Actor: ref})
	if status.Code(e) == codes.NotFound {
		got, e = c.control.CreateActorEgressPolicy(ctx, &pb.CreateActorEgressPolicyRequest{Actor: ref, EgressPolicy: want})
		if status.Code(e) == codes.AlreadyExists {
			got, e = c.control.GetActorEgressPolicy(ctx, &pb.GetActorEgressPolicyRequest{Actor: ref})
		}
	}
	if e != nil {
		return e
	}
	if got.GetMetadata().GetName() != "default" || got.GetMetadata().GetAtespace() != space || !proto.Equal(&pb.EgressPolicy{Rules: got.GetRules()}, &pb.EgressPolicy{Rules: want.Rules}) {
		return errors.New("runtime policy ownership conflict")
	}
	return nil
}
