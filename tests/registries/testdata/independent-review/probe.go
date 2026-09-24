package main
import("encoding/json";"os";"github.com/war-and-code/dircue/pkg/registries")
func main(){for _,s:=range []string{"registry='123'","registry='[]'","registry='{}'","registry='[1]'","registry='https://host'"}{r,e:=registries.Parse(".npmrc",[]byte(s));json.NewEncoder(os.Stdout).Encode(map[string]any{"input":s,"configuration":r,"error":e})}}
