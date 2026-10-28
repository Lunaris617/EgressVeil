//go:build linux

package main

import (
 "encoding/json"
 "errors"
 "os"
 "github.com/vishvananda/netlink"
 "ua3f-ttl-policy/internal/guard"
)

type ownedFeature struct { Interface string; Index int; Driver string; Enabled bool }
const originalFeature = stateDir + "/original-feature.json"
func rememberFeature(p featureProof) error {
 if _,e:=os.Stat(originalFeature);e==nil{return nil}
 if p.ListActive!=p.ListRequested{return errors.New("mixed rx-gro-list state requires review")}
 l,e:=netlink.LinkByName(lanPort);if e!=nil{return e}
 b,_:=json.Marshal(ownedFeature{lanPort,l.Attrs().Index,p.Driver,p.ListRequested})
 return os.WriteFile(originalFeature,b,0600)
}
func restoreOwnedFeature() error {
 b,e:=os.ReadFile(originalFeature);if os.IsNotExist(e){return nil};if e!=nil{return e}
 var o ownedFeature
 if json.Unmarshal(b,&o)!=nil{return errors.New("invalid feature ownership record")}
 l,e:=netlink.LinkByName(lanPort);if e!=nil{return e}
 p,e:=features(true);if e!=nil{return errors.New("feature changed externally; automatic restore refused")}
 if o.Interface!=lanPort || o.Index!=l.Attrs().Index || o.Driver!=p.Driver{return errors.New("interface identity changed; automatic restore refused")}
 if o.Enabled {
  if e=verifyHelper();e!=nil{return e}
  b,e=guard.Run(helperPath,"","--interface",lanPort,"--set-rx-gro-list","on","--apply")
  var result struct {Verified bool `json:"verified"`; NoChange bool `json:"no_change_needed"`; Error string `json:"error"`}
  if e!=nil || json.Unmarshal(b,&result)!=nil || result.Error!="" || (!result.Verified && !result.NoChange){return errors.New("feature restore not verified")}
 }
 return os.Remove(originalFeature)
}
