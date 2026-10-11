package management

import (
 "context"
 "crypto/rand"
 "crypto/sha256"
 "database/sql"
 "encoding/hex"
 "encoding/json"
 "errors"
 "fmt"
 "io"
 "net/http"
 "regexp"
 "strconv"
 "strings"
 "time"

 "github.com/avaargsh/gpu-compute-platform/internal/domain"
 "github.com/avaargsh/gpu-compute-platform/internal/store/agentstore"
)

var safeID=regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$`)

type Service struct {
 DB *sql.DB
 Auth Introspector
}

type mutationInput struct {
 ExpectedGeneration *int64 `json:"expectedGeneration"`
 PoolID string `json:"poolId,omitempty"`
 Spec json.RawMessage `json:"spec"`
}

func NewHandler(db *sql.DB, auth Introspector) (http.Handler,error) {
 if db==nil {return nil,errors.New("PostgreSQL is required; no in-memory management mutations")}
 if err:=auth.ValidateConfig();err!=nil {return nil,err}
 svc:=&Service{DB:db,Auth:auth}
 mux:=http.NewServeMux()
 mux.HandleFunc("GET /healthz",func(w http.ResponseWriter,_ *http.Request){w.Header().Set("Cache-Control","no-store");w.WriteHeader(204)})
 mux.HandleFunc("GET /api/v1/management/clusters/{clusterID}/projects/{projectID}/resources/{kind}/{resourceID}",svc.serve)
 mux.HandleFunc("PUT /api/v1/management/clusters/{clusterID}/projects/{projectID}/resources/{kind}/{resourceID}",svc.serve)
 mux.HandleFunc("DELETE /api/v1/management/clusters/{clusterID}/projects/{projectID}/resources/{kind}/{resourceID}",svc.serve)
 return mux,nil
}

func problem(w http.ResponseWriter, status int) {
 // Do not relay SQL errors or IdP details to callers.
 http.Error(w,http.StatusText(status),status)
}

func createRequestID() (string,error) {
 var bytes [16]byte
 if _,err:=rand.Read(bytes[:]);err!=nil{return "",err}
 return hex.EncodeToString(bytes[:]),nil
}

func (s *Service) serve(w http.ResponseWriter,r *http.Request) {
 w.Header().Set("Cache-Control","no-store")
 w.Header().Set("X-Content-Type-Options","nosniff")
 cluster,project,kind,id:=r.PathValue("clusterID"),r.PathValue("projectID"),r.PathValue("kind"),r.PathValue("resourceID")
 if !safeID.MatchString(cluster)||!safeID.MatchString(project)||!safeID.MatchString(id)||(kind!="Workload"&&kind!="ComputePool") {problem(w,400);return}
 token:=bearerToken(r)
 if token=="" {w.Header().Set("WWW-Authenticate","Bearer");problem(w,401);return}
 identity,err:=s.Auth.Verify(r.Context(),token)
 if err!=nil {w.Header().Set("WWW-Authenticate","Bearer");problem(w,401);return}
 if r.Method==http.MethodGet {
  s.read(w,r,identity,cluster,project,kind,id)
  return
 }
 if r.Header.Get("Origin")!="" || strings.EqualFold(r.Header.Get("Sec-Fetch-Site"),"cross-site") {
  problem(w,403);return
 }
 if !strings.HasPrefix(r.Header.Get("Content-Type"),"application/json") && r.Method==http.MethodPut {problem(w,415);return}
 requestID,err:=createRequestID()
 if err!=nil {problem(w,500);return}
 w.Header().Set("X-Request-ID",requestID)
 var input mutationInput
 if r.Method==http.MethodPut {
  r.Body=http.MaxBytesReader(w,r.Body,1<<18)
  dec:=json.NewDecoder(r.Body)
  dec.DisallowUnknownFields()
  if err:=dec.Decode(&input);err!=nil {problem(w,400);return}
  var extra any
  if err:=dec.Decode(&extra);err!=io.EOF {problem(w,400);return}
  if input.ExpectedGeneration==nil||*input.ExpectedGeneration<0||len(input.Spec)==0 {problem(w,400);return}
 }else{
  match:=r.Header.Get("If-Match")
  if len(match)<3||!strings.HasPrefix(match,"\"")||!strings.HasSuffix(match,"\""){problem(w,428);return}
  n,err:=strconv.ParseInt(match[1:len(match)-1],10,64)
  if err!=nil||n<1 {problem(w,400);return}
  input.ExpectedGeneration=&n
 }
 status,gen:=s.mutate(r.Context(),identity,cluster,project,kind,id,input,r.Method,requestID)
 if status!=0 {problem(w,status);return}
 w.Header().Set("ETag",fmt.Sprintf("\"%d\"",gen))
 if r.Method==http.MethodDelete {w.WriteHeader(http.StatusAccepted)} else {w.WriteHeader(http.StatusNoContent)}
}

func (s *Service) role(ctx context.Context,q interface{QueryRowContext(context.Context,string,...any)*sql.Row}, id Identity,project string,lock bool) (string,error) {
 query:=`SELECT role FROM management_project_grants WHERE issuer=$1 AND subject=$2 AND project_id=$3`
 if lock {query+=" FOR SHARE"}
 var role string
 err:=q.QueryRowContext(ctx,query,id.Issuer,id.Subject,project).Scan(&role)
 return role,err
}

func (s *Service) read(w http.ResponseWriter,r *http.Request,id Identity,cluster,project,kind,resourceID string) {
 role,err:=s.role(r.Context(),s.DB,id,project,false)
 if err==sql.ErrNoRows {problem(w,403);return}
 if err!=nil {problem(w,503);return}
 if role!="viewer"&&role!="operator"&&role!="admin"{problem(w,403);return}
 var spec []byte
 var gen int64
 var deletion sql.NullTime
 err=s.DB.QueryRowContext(r.Context(),`SELECT generation,spec,deletion_timestamp FROM desired_resources
 WHERE cluster_id=$1 AND kind=$2 AND resource_id=$3 AND spec->>'projectID'=$4`,cluster,kind,resourceID,project).Scan(&gen,&spec,&deletion)
 if err==sql.ErrNoRows {problem(w,404);return}
 if err!=nil {problem(w,503);return}
 w.Header().Set("Content-Type","application/json")
 w.Header().Set("ETag",fmt.Sprintf("\"%d\"",gen))
 _=json.NewEncoder(w).Encode(map[string]any{
  "clusterId":cluster,"projectId":project,"kind":kind,"id":resourceID,
  "generation":gen,"spec":json.RawMessage(spec),"deleting":deletion.Valid,
 })
}

func specHash(data []byte) string {
 digest:=sha256.Sum256(data)
 return hex.EncodeToString(digest[:])
}

func (s *Service) mutate(ctx context.Context,id Identity,cluster,project,kind,resourceID string,in mutationInput,method,requestID string)(int,int64) {
 tx,err:=s.DB.BeginTx(ctx,&sql.TxOptions{Isolation:sql.LevelSerializable})
 if err!=nil{return 503,0}
 defer tx.Rollback()
 role,err:=s.role(ctx,tx,id,project,true)
 if err==sql.ErrNoRows{return 403,0}
 if err!=nil{return 503,0}
 if role!="operator"&&role!="admin"{return 403,0}

 // Use the same lifecycle advisory key as the Agent Store; serialize
 // absent-row create with concurrent delete/finalize, not only existing rows.
 if _,err:=tx.ExecContext(ctx,`SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`,"desired/"+cluster+"/"+kind+"/"+resourceID);err!=nil{return 503,0}
 if kind=="Workload" {
  if _,err:=tx.ExecContext(ctx,`SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`,"Workload/"+resourceID);err!=nil{return 503,0}
 }

 var previousGen int64
 var previousSpec []byte
 var deletion sql.NullTime
 err=tx.QueryRowContext(ctx,`SELECT generation,spec,deletion_timestamp FROM desired_resources WHERE cluster_id=$1 AND kind=$2 AND resource_id=$3 FOR UPDATE`,cluster,kind,resourceID).Scan(&previousGen,&previousSpec,&deletion)
 if err!=nil&&err!=sql.ErrNoRows{return 503,0}
 exists:=err==nil
 if exists {
  var old struct{ProjectID string `json:"projectID"`}
  if json.Unmarshal(previousSpec,&old)!=nil||old.ProjectID!=project{return 404,0}
 }
 expected:=*in.ExpectedGeneration
 if !exists&&expected!=0 {return 409,0}
 if exists&&previousGen!=expected {return 409,0}
 if deletion.Valid{return 409,0}
 if method==http.MethodDelete&&!exists{return 404,0}
 if method==http.MethodPut&&kind=="Workload"&&exists{return 409,0} // immutable Workload: delete/recreate only

 var newSpec []byte
 var newGen int64
 action:="delete-request"
 if method==http.MethodPut {
  if !exists {
   var tombstone int64
   er:=tx.QueryRowContext(ctx,`SELECT generation FROM deletion_tombstones WHERE cluster_id=$1 AND kind=$2 AND resource_id=$3`,cluster,kind,resourceID).Scan(&tombstone)
   if er==nil{return 409,0} // avoid reusing finalized identity through this initial API
   if er!=sql.ErrNoRows{return 503,0}
  }
  newSpec,err=s.buildSpec(ctx,tx,cluster,project,kind,resourceID,in)
  if err!=nil {
   if errors.Is(err,sql.ErrNoRows){return 409,0}
   if errors.Is(err,errInvalidSpec){return 400,0}
   if errors.Is(err,errBinding){return 409,0}
   return 503,0
  }
  if exists {newGen=previousGen+1;action="update"}else{newGen=1;action="create"}
  if newGen<1{return 409,0}
  if exists {
   result,e:=tx.ExecContext(ctx,`UPDATE desired_resources SET generation=$4,spec=$5,updated_at=now()
     WHERE cluster_id=$1 AND kind=$2 AND resource_id=$3 AND generation=$6 AND deletion_timestamp IS NULL`,
    cluster,kind,resourceID,newGen,newSpec,expected)
   if e!=nil{return 503,0}
   n,e:=result.RowsAffected();if e!=nil{return 503,0};if n!=1{return 409,0}
  }else{
   _,err=tx.ExecContext(ctx,`INSERT INTO desired_resources (cluster_id,kind,resource_id,generation,spec) VALUES ($1,$2,$3,$4,$5)`,cluster,kind,resourceID,newGen,newSpec)
   if err!=nil{return 409,0}
  }
 }else{
  newGen=previousGen
  newSpec=previousSpec
  if kind=="ComputePool" {
   // Refuse pool teardown while workloads still refer to it.
   var n int
   e:=tx.QueryRowContext(ctx,`SELECT count(*) FROM desired_resources WHERE cluster_id=$1 AND kind='Workload' AND spec->>'poolID'=$2`,cluster,resourceID).Scan(&n)
   if e!=nil{return 503,0}
   if n!=0{return 409,0}
  }
  finalizers,_:=json.Marshal([]string{agentstore.ProviderCleanupFinalizer})
  result,e:=tx.ExecContext(ctx,`UPDATE desired_resources SET deletion_timestamp=now(),
   finalizers=CASE WHEN finalizers ? $4 THEN finalizers ELSE finalizers || $5::jsonb END,
   updated_at=now() WHERE cluster_id=$1 AND kind=$2 AND resource_id=$3 AND generation=$6 AND deletion_timestamp IS NULL`,
   cluster,kind,resourceID,agentstore.ProviderCleanupFinalizer,string(finalizers),previousGen)
  if e!=nil{return 503,0}
  n,e:=result.RowsAffected();if e!=nil{return 503,0};if n!=1{return 409,0}
 }
 // This append-only audit event commits atomically with the Desired change.
 // An audit insert failure rolls back the *entire* write and CAS outcome.
 _,err=tx.ExecContext(ctx,`INSERT INTO management_operation_audit
 (request_id,issuer,subject,project_id,cluster_id,kind,resource_id,action,expected_generation,resulting_generation,prior_spec_sha256,resulting_spec_sha256)
 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
 requestID,id.Issuer,id.Subject,project,cluster,kind,resourceID,action,expected,newGen,specHash(previousSpec),specHash(newSpec))
 if err!=nil{return 503,0}
 if err:=tx.Commit();err!=nil{return 409,0} // serializable conflict, commit state uncertain only if connection failed; no replay guarantees
 return 0,newGen
}

var errInvalidSpec=errors.New("invalid resource spec")
var errBinding=errors.New("invalid resource binding")

func (s *Service) buildSpec(ctx context.Context,tx *sql.Tx,cluster,project,kind,resourceID string,in mutationInput)([]byte,error) {
 var projectCluster,namespace string
 err:=tx.QueryRowContext(ctx,`SELECT cluster_id,namespace FROM project_bindings WHERE project_id=$1 FOR SHARE`,project).Scan(&projectCluster,&namespace)
 if err!=nil{return nil,err}
 if projectCluster!=cluster{return nil,errBinding}
 var spec map[string]any
 if kind=="ComputePool" {
  var pool domain.ComputePoolSpec
  if err:=strictJSON(in.Spec,&pool);err!=nil{return nil,errInvalidSpec}
  if len(pool.Accelerators)==0||len(pool.AcceleratorBindings)==0||pool.Scheduling.Mode!="kueue"{return nil,errInvalidSpec}
  classes:=map[string]bool{}
  for _,binding:=range pool.AcceleratorBindings {
   if binding.Class==""||binding.ResourceName==""||binding.Flavor==""||binding.AllocationMode==domain.AcceleratorAllocationDRA{return nil,errInvalidSpec}
   classes[binding.Class]=true
  }
  for _,acc:=range pool.Accelerators{if acc.Class==""||acc.Quota<=0||!classes[acc.Class]{return nil,errInvalidSpec}}
  var poolCluster,provider string
  e:=tx.QueryRowContext(ctx,`SELECT cluster_id,provider FROM cluster_bindings WHERE pool_id=$1 FOR SHARE`,resourceID).Scan(&poolCluster,&provider)
  if e!=nil{return nil,e}
  if poolCluster!=cluster||provider!="kueue"{return nil,errBinding}
  spec=map[string]any{"projectID":project,"namespace":namespace,"provider":provider,"accelerators":pool.Accelerators,"acceleratorBindings":pool.AcceleratorBindings,"scheduling":pool.Scheduling}
 }else{
  var work domain.WorkloadSpec
  if err:=strictJSON(in.Spec,&work);err!=nil{return nil,errInvalidSpec}
  if !safeID.MatchString(in.PoolID)||work.Image==""||work.Accelerator.Class==""||work.Accelerator.Quota<=0{return nil,errInvalidSpec}
  var poolCluster,provider string
  e:=tx.QueryRowContext(ctx,`SELECT cluster_id,provider FROM cluster_bindings WHERE pool_id=$1 FOR SHARE`,in.PoolID).Scan(&poolCluster,&provider)
  if e!=nil{return nil,e}
  if poolCluster!=cluster||provider!="kueue"{return nil,errBinding}
  var poolSpec []byte
  var deleting sql.NullTime
  e=tx.QueryRowContext(ctx,`SELECT spec,deletion_timestamp FROM desired_resources WHERE cluster_id=$1 AND kind='ComputePool' AND resource_id=$2 FOR SHARE`,cluster,in.PoolID).Scan(&poolSpec,&deleting)
  if e!=nil{return nil,e}
  if deleting.Valid{return nil,errBinding}
  var parent struct{
   ProjectID string `json:"projectID"`
   Bindings []domain.AcceleratorBinding `json:"acceleratorBindings"`
  }
  if json.Unmarshal(poolSpec,&parent)!=nil||parent.ProjectID!=project{return nil,errBinding}
  matched:=false
  for _,b:=range parent.Bindings{if b.Class==work.Accelerator.Class{matched=true}}
  if !matched{return nil,errBinding}
  spec=map[string]any{"projectID":project,"poolID":in.PoolID,"namespace":namespace,"provider":provider,"image":work.Image,"command":work.Command,"accelerator":work.Accelerator}
 }
 return json.Marshal(spec)
}

func strictJSON(raw []byte,target any) error {
 decoder:=json.NewDecoder(strings.NewReader(string(raw)))
 decoder.DisallowUnknownFields()
 if err:=decoder.Decode(target);err!=nil{return err}
 var extra any
 if err:=decoder.Decode(&extra);err!=io.EOF{return errInvalidSpec}
 return nil
}
