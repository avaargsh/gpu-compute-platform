package management

import (
 "context"
 "encoding/json"
 "errors"
 "fmt"
 "io"
 "net/http"
 "net/url"
 "strings"
 "time"
)

// RFC 7662 token introspection delegates cryptographic token validation and
// revocation to an OIDC provider. Do NOT parse JWT payloads as identity or
// trust forwarded user headers. The provider MUST return iss, sub, exp, aud.
type Introspector struct {
 URL string
 Issuer string
 Audience string
 ClientID string
 ClientSecret string
 Client *http.Client
 AllowHTTPForTests bool
}

type Identity struct { Issuer, Subject string }
type introspectionResult struct {
 Active bool `json:"active"`
 Issuer string `json:"iss"`
 Subject string `json:"sub"`
 ExpiresAt int64 `json:"exp"`
 Audience json.RawMessage `json:"aud"`
 TokenType string `json:"token_type"`
}

func (i Introspector) ValidateConfig() error {
 u,err:=url.Parse(i.URL)
 if err!=nil || u.Host=="" || u.User!=nil || u.Fragment!="" || (u.Scheme!="https" && !(i.AllowHTTPForTests && u.Scheme=="http")) { return errors.New("OIDC introspection requires an absolute HTTPS URL") }
 issuer,err:=url.Parse(i.Issuer)
 if err!=nil || issuer.Scheme!="https" || issuer.Host=="" || issuer.RawQuery!="" || issuer.Fragment!="" { return errors.New("OIDC issuer must be an absolute HTTPS URL") }
 if i.Audience=="" || i.ClientID=="" || i.ClientSecret=="" { return errors.New("OIDC audience and introspection client credentials are required") }
 return nil
}

func (i Introspector) Verify(ctx context.Context, token string) (Identity,error) {
 if err:=i.ValidateConfig();err!=nil { return Identity{},err }
 if len(token)<8 || len(token)>8192 || strings.ContainsAny(token," \r\n\t") { return Identity{},errors.New("invalid access token") }
 form:=url.Values{"token":{token},"token_type_hint":{"access_token"}}
 req,err:=http.NewRequestWithContext(ctx,http.MethodPost,i.URL,strings.NewReader(form.Encode()))
 if err!=nil{return Identity{},err}
 req.Header.Set("Content-Type","application/x-www-form-urlencoded")
 req.Header.Set("Accept","application/json")
 req.SetBasicAuth(i.ClientID,i.ClientSecret)
 client:=i.Client
 if client==nil {client=&http.Client{Timeout:5*time.Second, CheckRedirect:func(_ *http.Request,_ []*http.Request)error{return http.ErrUseLastResponse}}}
 resp,err:=client.Do(req)
 if err!=nil {return Identity{},fmt.Errorf("identity provider unavailable: %w",err)}
 defer resp.Body.Close()
 if resp.StatusCode!=http.StatusOK {return Identity{},errors.New("identity provider did not validate token")}
 var result introspectionResult
 dec:=json.NewDecoder(io.LimitReader(resp.Body,8193))
 if err:=dec.Decode(&result);err!=nil{return Identity{},errors.New("invalid identity provider response")}
 if !result.Active || result.Issuer!=i.Issuer || result.Subject=="" || len(result.Subject)>512 || result.ExpiresAt<=time.Now().Unix() || result.TokenType!="" && !strings.EqualFold(result.TokenType,"Bearer") {
  return Identity{},errors.New("inactive, expired, or untrusted identity")
 }
 var single string
 if err:=json.Unmarshal(result.Audience,&single);err==nil {
  if single!=i.Audience{return Identity{},errors.New("wrong token audience")}
 } else {
  var values []string
  if err:=json.Unmarshal(result.Audience,&values);err!=nil{return Identity{},errors.New("missing token audience")}
  matched:=false
  for _,v:=range values {if v==i.Audience{matched=true}}
  if !matched{return Identity{},errors.New("wrong token audience")}
 }
 return Identity{Issuer:i.Issuer,Subject:result.Subject},nil
}

func bearerToken(r *http.Request) string {
 values:=r.Header.Values("Authorization")
 if len(values)!=1 || !strings.HasPrefix(values[0],"Bearer "){return ""}
 token:=strings.TrimPrefix(values[0],"Bearer ")
 if token=="" || strings.ContainsAny(token," \r\n\t"){return ""}
 return token
}
