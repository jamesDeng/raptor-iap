package agentaccess
import("encoding/json";"os";"path/filepath";"strings";"testing";"time";"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain")
func TestExportQuestionContract(t *testing.T){
 dir:=os.Getenv("CONTRACT_FIXTURE_DIR");if dir==""{return}
 def:=domain.RequestInput{Type:"agent",Model:"gpt-5.6-luna",Object:domain.ObjectRef{Kind:"application",Code:"app"},EnvCode:"rdev.ali",Operations:[]domain.Operation{{Name:"application.question",Parameters:map[string]any{"question":"Is gateway healthy?"}}},Skills:domain.SkillsVersion{Tag:"v1",CommitSHA:strings.Repeat("a",40)}}
 hash,e:=Fingerprint(def);if e!=nil{t.Fatal(e)}
 request:="11111111-1111-4111-8111-111111111111";attempt:="22222222-2222-4222-8222-222222222222"
 context:=map[string]any{"requestId":request,"definition":def,"skills":def.Skills,"environment":map[string]any{"code":"rdev.ali","config":map[string]any{"ackClusterId":"cluster"}},"definitionSha256":hash}
 issued:=Issued{Credential:"raptor_at_"+strings.Repeat("q",43),ExpiresAt:time.Date(2099,1,1,0,0,0,0,time.UTC),Binding:Binding{RequestID:request,AttemptID:attempt,Operation:"application.question",ObjectKind:"application",ObjectCode:"app",EnvCode:"rdev.ali",SkillsCommit:def.Skills.CommitSHA,Model:def.Model,DefinitionSHA256:hash,ClusterID:"cluster"},RaptorMCPURL:"https://raptor.fixture/mcp",InfraMCPURL:"https://infra.fixture/mcp"}
 for name,v:=range map[string]any{"raptor-context.json":context,"raptor-access.json":map[string]any{"data":issued}}{raw,e:=json.MarshalIndent(v,"","  ");if e!=nil{t.Fatal(e)};if e=os.WriteFile(filepath.Join(dir,name),append(raw,'\n'),0644);e!=nil{t.Fatal(e)}}
}
