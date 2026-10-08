package substrate

import (
	"context"
	pb "github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"reflect"
	"testing"
)

type egressFake struct {
	pb.ControlClient
	policy  *pb.EgressPolicy
	creates int
}

func (f *egressFake) GetActorEgressPolicy(context.Context, *pb.GetActorEgressPolicyRequest, ...grpc.CallOption) (*pb.EgressPolicy, error) {
	if f.policy == nil {
		return nil, status.Error(codes.NotFound, "absent")
	}
	return f.policy, nil
}
func (f *egressFake) CreateActorEgressPolicy(_ context.Context, r *pb.CreateActorEgressPolicyRequest, _ ...grpc.CallOption) (*pb.EgressPolicy, error) {
	f.creates++
	f.policy = r.EgressPolicy
	return f.policy, nil
}
func TestRuntimeEgressIsExactTLSAllowlist(t *testing.T) {
	f := &egressFake{}
	c := Client{control: f}
	hosts := []string{"auth.openai.com", "chatgpt.com", "api.rdev.raptor-iap.top"}
	if e := c.EnsureRuntimeEgress(context.Background(), "runtime", "actor", hosts); e != nil {
		t.Fatal(e)
	}
	if f.creates != 1 || len(f.policy.Rules) != 1 {
		t.Fatal("missing policy")
	}
	r := f.policy.Rules[0].TlsPassthrough
	if r == nil || !reflect.DeepEqual(r.Ports.Numbers, []int32{443}) || len(r.Hostnames) != 3 {
		t.Fatal("egress not restricted to TLS443")
	}
	if e := c.EnsureRuntimeEgress(context.Background(), "runtime", "actor", hosts); e != nil || f.creates != 1 {
		t.Fatal("idempotent policy changed")
	}
	if e := c.EnsureRuntimeEgress(context.Background(), "runtime", "actor", []string{"other.example"}); e == nil {
		t.Fatal("existing policy broadened")
	}
}
func TestRuntimeEgressRejectsWildcardIPAndPrivateNames(t *testing.T) {
	for _, host := range []string{"*", "*.openai.com", "10.70.1.180", "atenet-router.ate-system.svc", "localhost", "api.example:8443"} {
		c := Client{control: &egressFake{}}
		if e := c.EnsureRuntimeEgress(context.Background(), "runtime", "actor", []string{host}); e == nil {
			t.Fatalf("unsafe domain %s accepted", host)
		}
	}
}
