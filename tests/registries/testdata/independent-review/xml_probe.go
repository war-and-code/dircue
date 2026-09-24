package main
import("encoding/json";"os";"github.com/war-and-code/dircue/pkg/registries")
func main(){for _,s:=range []string{`<?xml version="1.0"?><?xml version="1.0"?><configuration/>`,`<?xml garbage?><configuration/>`,`<!-- comment --><?xml version="1.0"?><configuration/>`,`<?xml?><configuration/>`}{r,e:=registries.Parse("NuGet.Config",[]byte(s));json.NewEncoder(os.Stdout).Encode(map[string]any{"input":s,"configuration":r,"error":e})}}
