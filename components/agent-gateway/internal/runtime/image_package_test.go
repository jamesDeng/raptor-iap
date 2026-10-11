package runtime
import("os";"strings";"testing")
func TestGatewayImageIncludesEveryDeliveredHarnessModule(t *testing.T){
 raw,e:=os.ReadFile("../../Dockerfile");if e!=nil{t.Fatal(e)}
 for _,name:=range harnessFiles{if !strings.Contains(string(raw),"/"+name+" "){t.Errorf("Gateway image missing harness module: %s",name)}}
}
