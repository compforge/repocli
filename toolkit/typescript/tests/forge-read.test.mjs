import assert from "node:assert/strict";
import { test } from "node:test";
import { GitHubForgeReader, GitLabForgeReader } from "../dist/index.js";

const stamp = n => new Date(Date.UTC(2026, 9, 26, 0, n)).toISOString();
const key = (number=1) => ({ repository: { forge: { name: "work" }, path: "org/repo" }, number });
function fixture({ sameTime=false }={}) {
  let now = stamp(120), failSearch = false, failNumber;
  const rows = Array.from({length:51}, (_,i)=>({iid:i+1, author:{username:"me"}, updated_at:stamp(sameTime?1:i+1),
    web_url:`https://gitlab.test/org/repo/-/merge_requests/${i+1}`, title:"Proposal", state:"merged", sha:"a"}));
  const requests=[];
  const reader = new GitLabForgeReader({forge:"work",host:"gitlab.test",provider:"gitlab",token:"fixture"}, {
    now:()=>new Date(now), fetch:async input=>{
      const url=new URL(String(input));requests.push(url);
      if(url.pathname==="/api/v4/merge_requests") {
        if(failSearch)return new Response("search unavailable",{status:503});
        const from=url.searchParams.get("updated_after")??"", to=url.searchParams.get("updated_before");
        const matching=rows.filter(r=>r.updated_at>=from && r.updated_at<=to && (url.searchParams.get("state")==="all" || r.state==="opened"));
        return Response.json(matching.slice(0,50),{headers:matching.length>50?{"x-next-page":"2"}:{}});
      }
      if(url.pathname.endsWith("/discussions"))return Response.json([]);
      const n=Number(url.pathname.split("/").pop());
      if(n===failNumber)return new Response("temporarily unavailable",{status:503});
      return Response.json(rows.find(r=>r.iid===n));
    },
  });
  return {reader,rows,requests,setNow:v=>now=v,setFailure:v=>failSearch=v,setReadFailure:v=>failNumber=v};
}

// @case A previously returned item moves beyond the frozen window; all unseen merged MRs remain discoverable.
test("split windows preserve coverage when earlier rows move; opaque cursors survive recreation", async()=>{
  const f=fixture(), seen=new Set();let cursor, result, moved=false;
  for(let round=0;round<80;round++) {
    result=await f.reader.readPullRequests({author:"me",since:stamp(0),cursor});
    assert.deepEqual(result.errors,[]);
    for(const item of result.items)seen.add(item.number);
    cursor=JSON.parse(JSON.stringify(result.cursor));
    if(!moved && seen.size) { f.rows[0].updated_at=stamp(121);moved=true; }
    if(!result.pending)break;
  }
  assert.equal(result.pending,false);
  assert.equal(seen.size,51);
  assert(f.requests.filter(u=>u.pathname==="/api/v4/merge_requests").every(u=>u.searchParams.get("page")==="1"));
  f.setNow(stamp(125));
  const next=await f.reader.readPullRequests({author:"me",cursor});
  assert(next.items.some(p=>p.number===1));
  await assert.rejects(f.reader.readPullRequests({author:"another",cursor}),/account/);
});

test("an overfull minimum window reports missing coverage and still reads tracked terminal changes", async()=>{
  const f=fixture({sameTime:true});let cursor,result;
  for(let i=0;i<80;i++) {
    result=await f.reader.readPullRequests({author:"me",since:stamp(0),cursor,tracked:[key(51)]});
    cursor=result.cursor;
    if(result.errors.length)break;
  }
  assert.equal(result.pending,true);
  assert(result.errors.some(e=>e.includes("cannot prove coverage")));
  // A failed discovery window must not monopolize known-PR refresh on subsequent calls.
  f.rows[50].sha="merged-head";
  result=await f.reader.readPullRequests({author:"me",cursor,tracked:[key(51)]});
  assert(result.items.some(p=>p.number===51 && p.headSha==="merged-head"));
});

test("discovery outage does not hide a known merge; a failed detail retains its page position", async()=>{
  const f=fixture();f.setFailure(true);
  let result=await f.reader.readPullRequests({author:"me",since:stamp(0),tracked:[key()]});
  assert.equal(result.items[0].state,"merged");assert(result.errors.length);
  f.setFailure(false);let cursor=result.cursor;
  f.setReadFailure(1);
  for(let i=0;i<30;i++) {
    result=await f.reader.readPullRequests({author:"me",cursor});cursor=result.cursor;
    if(result.errors.length)break;
  }
  assert(result.errors.length);assert(result.pending);
  f.setReadFailure(undefined);
  result=await f.reader.readPullRequests({author:"me",cursor});
  assert(result.items.some(p=>p.number===1));
});

test("Github search overflow is split instead of advancing a capped window", async()=>{
  let calls=0;
  const reader=new GitHubForgeReader({forge:"github.com",host:"github.com",provider:"github",token:"fixture"},{now:()=>new Date(stamp(120)),fetch:async input=>{
    calls++;
    return Response.json({incomplete_results:false,total_count:calls===1?1200:0,items:[]});
  }});
  const result=await reader.readPullRequests({author:"me",since:stamp(0)});
  assert.deepEqual(result.errors,[]);assert(calls>1);
});

test("no conflicts, readiness and merged are independent on both platforms", async()=>{
  let status="BLOCKED", draft=false, state="open";
  const github=new GitHubForgeReader({forge:"github.com",host:"github.com",provider:"github",token:"fixture"},{fetch:async input=>{
    if(String(input).endsWith("/graphql"))return Response.json({data:{repository:{pullRequest:{isDraft:draft,mergeStateStatus:status,reviewThreads:{nodes:[],pageInfo:{hasNextPage:false}}}}}});
    return Response.json({title:"PR",html_url:"https://github.com/org/repo/pull/1",state,merged:state==="closed",mergeable:true});
  }});
  const identity={...key(),repository:{...key().repository,forge:{name:"github.com"}}};
  let pr=await github.get(identity);
  assert.equal(pr.conflictState,"none");assert.equal(pr.mergeReadiness,"blocked");assert.equal(pr.state,"open");
  status="CLEAN";pr=await github.get(identity);assert.equal(pr.mergeReadiness,"ready");assert.equal(pr.state,"open");
  draft=true;pr=await github.get(identity);assert.equal(pr.mergeReadiness,"draft");
  draft=false;status="UNKNOWN";state="closed";pr=await github.get(identity);assert.equal(pr.state,"merged");assert.equal(pr.mergeReadiness,"unknown");
  let detail="not_approved";
  const gitlab=new GitLabForgeReader({forge:"work",host:"gitlab.test",provider:"gitlab",token:"fixture"},{fetch:async input=>String(input).includes("discussions")?Response.json([]):Response.json({title:"MR",web_url:"https://gitlab.test/org/repo/-/merge_requests/1",state:"opened",has_conflicts:false,merge_status:"can_be_merged",detailed_merge_status:detail})});
  pr=await gitlab.get(key());assert.equal(pr.conflictState,"none");assert.equal(pr.mergeReadiness,"needs_approval");
  detail="ci_must_pass";pr=await gitlab.get(key());assert.equal(pr.mergeReadiness,"ci_blocked");
});


test("aggregate HTTP deadlines bound a read without cancelling concurrent operations", async()=>{
  const { JsonHttpClient } = await import("../dist/forge/http.js");
  const http = new JsonHttpClient({timeoutMs:500,fetch:async (_input, init)=>{
    await new Promise((resolve,reject)=>{
      const timer=setTimeout(resolve,100);
      init.signal.addEventListener("abort",()=>{clearTimeout(timer);reject(new Error("Aborted"));},{once:true});
    });
    return Response.json({ok:true});
  }});
  const [bounded, independent] = await Promise.allSettled([
    http.withinDeadline(10,()=>http.request("GET","https://fixture.invalid/bounded")),
    http.request("GET","https://fixture.invalid/independent"),
  ]);
  assert.equal(bounded.status,"rejected");
  assert.equal(independent.status,"fulfilled");
});
