package registries

import (
 "encoding/json"
 "net/url"
 "reflect"
 "strings"
 "testing"
)

func TestIndependentAuditEndpointDisclosure(t *testing.T) {
 const marker = "AUTH_SENTINEL_51db0"
 values := []string{
  "https://user:"+marker+"@feed.example/path?key="+marker+"#"+marker,
  "https://user%3a"+marker+"@feed.example/",
  "https://user%40"+marker+"@feed.example/",
  "https://feed.example/"+marker+"?other=1",
  "https://feed.example?key="+marker,
  "https://feed.example#"+marker,
  "https://feed.example%2f"+marker+"/",
  "https://feed.example%40"+marker+"/",
  "https://feed.example\\@"+marker+".invalid/",
  "https://feed.example\u202e/"+marker,
  "https://feed.example\u0085/"+marker,
  "https://user:"+marker+"@[::1]:443/private",
  "https://user:"+marker+"@[fe80::1%25en0]/",
  "https://feed.example/$"+marker,
  "https://%"+marker+"%/",
  "C:\\"+marker+"\\feed",
  "../"+marker+"/feed",
 }
 for _, input := range values {
  got:=endpoint(input);blob,_:=json.Marshal(got)
  if strings.Contains(string(blob),marker){t.Fatalf("disclosed marker for%q: %s",input,blob)}
  if got.Origin!=""{parsed,err:=url.Parse(got.Origin);if err!=nil||parsed.User!=nil||parsed.Path!=""||parsed.RawQuery!=""||parsed.Fragment!=""||parsed.Host==""{t.Fatalf("not origin: %+v",got)}}
 }
}

func TestIndependentAuditQualifiedNamesAreDisclosedByDesign(t *testing.T){
 const name="InternalTeamFeed"
 content:=[]byte(`<configuration><packageSources><add key="`+name+`" value="https://internal.example/path"/></packageSources></configuration>`)
 got,err:=Parse("NuGet.Config",content);if err!=nil||len(got.Declarations)!=1||got.Declarations[0].Name.Value!=name{t.Fatalf("%+v %v",got,err)}
 before,_:=json.Marshal(got);for i:=range content{content[i]='x'};after,_:=json.Marshal(got);if !reflect.DeepEqual(before,after){t.Fatal("report aliases caller input")}
}

func TestIndependentAuditXMLNamespaceAndDirectiveTransactions(t *testing.T){
 prefix:=`<packageSources><add key="Feed" value="https://feed.example/path"/></packageSources>`
 for _,input:=range []string{
  `<configuration>`+prefix+`<packageSources xmlns=""><add key="Other" value="https://other.example"/></packageSources></configuration>`,
  `<configuration xmlns:x="urn:example">`+prefix+`</configuration>`,
  `<configuration>`+prefix+`<?target AUTH_SENTINEL_51db0?></configuration>`,
  `<!DOCTYPE configuration [<!ENTITY leak "AUTH_SENTINEL_51db0">]><configuration>`+prefix+`</configuration>`,
  `<configuration>`+prefix+`<packageSources><add key="Feed" value="&unknown;"/></packageSources></configuration>`,
 }{
  got,err:=Parse("NuGet.Config",[]byte(input));if err!=nil||got.Status!="partial"||got.DeclarationCountComplete||len(got.Declarations)!=0{t.Fatalf("not transactional: %+v %v",got,err)}
  blob,_:=json.Marshal(got);if strings.Contains(string(blob),"AUTH_SENTINEL_51db0"){t.Fatal("diagnostic exposed input")}
 }
}

func TestIndependentAuditNPMOracleTypedMatrix(t *testing.T){
 for _,value:=range []string{"123","[]","{}","[1]","true","false","null","1e400"}{
  for _,quote:=range []string{"","'","\""}{
   input:="registry="+quote+value+quote
   got,err:=Parse(".npmrc",[]byte(input));if err!=nil{t.Fatal(err)}
   typed:=quote=="'"||value=="true"||value=="false"||value=="null"
   if typed{if got.Status!="partial"||len(got.Declarations)!=0||got.Omissions["unsupported_npm_value"]!=1{t.Fatalf("typed ini value interpreted as registry: %q %+v",input,got)}}else if got.Status!="complete"||len(got.Declarations)!=1||got.Declarations[0].Endpoint.Status!="local_path"{t.Fatalf("string ini value changed: %q %+v",input,got)}
  }
 }
 for _,input:=range []string{`registry='"true"'`,`registry='"false"'`,`registry='"null"'`}{
  got,_:=Parse(".npmrc",[]byte(input));if got.Status!="partial"||len(got.Declarations)!=0{t.Fatalf("nested typed string accepted: %+v",got)}
 }
 got,_:=Parse(".npmrc",[]byte(`registry='"https://user:AUTH_SENTINEL_51db0@feed.example/private"'`));if got.Status!="complete"||len(got.Declarations)!=1||got.Declarations[0].Endpoint.Origin!="https://feed.example"{t.Fatalf("nested string URL failed: %+v",got)}
}
