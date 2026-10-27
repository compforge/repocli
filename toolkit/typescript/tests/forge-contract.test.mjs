import assert from "node:assert/strict";
import test from "node:test";
import { GitHubForgeReader, GitLabForgeReader, iteratePullRequests, ForgeCoverageError } from "../dist/index.js";
import { scanPullRequests } from "../dist/forge/scan.js";
import { ForgeNotFound, ForgeRateLimitError } from "../dist/index.js";
const repository = { forge: {name:"fixture"}, path:"org/repo" };
const key = number => ({repository, number});
const account = {kind:"account",author:"me"};
const repoScope = {kind:"repository",repository};
const now = new Date("2026-10-27T12:00:00Z");
const config = provider => ({forge:"fixture",provider,host: provider === "github" ? "github.com" : "gitlab.test",token:"fixture-token"});
const row = (provider,number) => provider === "github"
  ? {number,user:{login:"me"},pull_request:{},repository_url:"https://api.github.com/repos/org/repo"}
  : {iid:number,author:{username:"me"},web_url:`https://gitlab.test/org/repo/merge_requests/${number}`};
const payload = (provider,rows,total=rows.length) => provider === "github" ? {items:rows,total_count:total,incomplete_results:false} : rows;
const make = (provider,fetch,overrides={}) => new (provider === "github" ? GitHubForgeReader : GitLabForgeReader)({...config(provider),...overrides},{fetch});

for (const provider of ["github","gitlab"]) {
  test(`${provider} scoped pages retain continuation, author and lifecycle filters`, async () => {
    const urls=[];
    const reader=make(provider,async input=>{
      const url=new URL(input);urls.push(url);
      const page=Number(url.searchParams.get("page"));
      return Response.json(payload(provider,[row(provider,page)],2),{headers:page===1
        ? provider==="github" ? {link:'<https://untrusted.invalid/?page=2>; rel="next"'} : {"x-next-page":"2"}
        : {}});
    });
    const query={scope:{...repoScope,author:"me"},state:"merged",pageSize:2};
    const first=await reader.listPullRequests(query);
    assert.deepEqual(first.items,[key(1)]);assert(first.nextCursor);
    const second=await reader.listPullRequests({...query,cursor:first.nextCursor});
    assert.deepEqual(second.items,[key(2)]);assert.equal(second.nextCursor,undefined);
    assert.equal(urls[1].searchParams.get("page"),"2");
    if(provider==="github") {
      assert.match(urls[0].searchParams.get("q"),/repo:org\/repo author:me is:merged/);
      await reader.listPullRequests({...query,state:"closed"});
      assert.match(urls[2].searchParams.get("q"),/is:closed is:unmerged/);
    } else {
      assert.equal(urls[0].pathname,"/api/v4/projects/org%2Frepo/merge_requests");
      assert.equal(urls[0].searchParams.get("author_username"),"me");
      assert.equal(urls[0].searchParams.get("state"),"merged");
    }
    const calls=urls.length;
    for(const changed of [{scope:account},{scope:{...repoScope,author:"another"}},{state:"open"},{pageSize:3},{updatedAfter:now.toISOString()}])
      await assert.rejects(reader.listPullRequests({...query,...changed,cursor:first.nextCursor}),/cursor/);
    for(const override of [{token:"other"},{host:"other.example"},{forge:"other"}])
      await assert.rejects(make(provider,()=>{throw Error("must not request");},override).listPullRequests({...query,cursor:first.nextCursor}));
    assert.equal(urls.length,calls);
    assert(!first.nextCursor.includes("fixture-token"));
  });
  test(`${provider} malformed rows retain healthy keys and a coverage issue`,async()=>{
    const reader=make(provider,async()=>Response.json(payload(provider,[row(provider,1),null,row(provider,2)])));
    const page=await reader.listPullRequests({scope:account});
    assert.deepEqual(page.items,[key(1),key(2)]);
    assert.equal(page.issues.length,1);assert.equal(page.issues[0].kind,"invalid_data");
    await assert.rejects(async()=>{for await(const _ of iteratePullRequests(reader,{scope:account})){}},ForgeCoverageError);
  });
}

test("GitLab account identity supports both URL styles and rejects foreign/malformed identities",async()=>{
  const rows=[row("gitlab",1),{...row("gitlab",2),web_url:"https://gitlab.test/org/repo/-/merge_requests/2"},
    {...row("gitlab",3),web_url:"https://elsewhere.test/org/repo/merge_requests/3"},
    {...row("gitlab",4),web_url:"https://gitlab.test/org/repo/merge_requests/5"},
    {...row("gitlab",6),web_url:"https://gitlab.test/org/%2E%2E/repo/merge_requests/6"}];
  const reader=make("gitlab",async()=>Response.json(rows));
  const page=await reader.listPullRequests({scope:account});
  assert.deepEqual(page.items,[key(1),key(2)]);assert.equal(page.issues.length,3);
  const scoped=make("gitlab",async()=>Response.json([{iid:7}]));
  assert.deepEqual((await scoped.listPullRequests({scope:repoScope})).items,[key(7)]);
});

test("GitLab honors the exact increasing next-page header",async()=>{
  const urls=[];
  const reader=make("gitlab",async input=>{urls.push(new URL(input));return Response.json([],{headers:{"x-next-page":"4"}});});
  const first=await reader.listPullRequests({scope:account});
  await assert.rejects(reader.listPullRequests({scope:account,cursor:first.nextCursor}),/did not advance/);
  assert.equal(urls[1].searchParams.get("page"),"4");
});

test("GitHub merged snapshot never invokes reviews, including when review requests are rate limited",async()=>{
  let reviewCalls=0;
  const reader=make("github",async input=>{
    if(String(input).endsWith("/pulls/1"))return Response.json({number:1,state:"closed",merged:true,title:"Done",html_url:"https://github.com/org/repo/pull/1"});
    reviewCalls++;return new Response("limited",{status:429,headers:{"retry-after":"60"}});
  });
  assert.equal((await reader.getPullRequest(key(1))).state,"merged");assert.equal(reviewCalls,0);
  await assert.rejects(reader.getPullRequestReview(key(1)),ForgeRateLimitError);
});

test("PullRequest iteration is lazy, survives empty intermediate pages and stops without prefetch",async()=>{
  let calls=0;
  const reader={listPullRequests:async query=>{calls++;return calls===1 ? {items:[],nextCursor:"opaque"} : {items:[key(1),key(2)],nextCursor:"later"};}};
  const iterator=iteratePullRequests(reader,{scope:account});assert.equal(calls,0);
  for await(const item of iterator){assert.deepEqual(item,key(1));break;}
  assert.equal(calls,2);
});

function scanFixture() {
  let unavailable = new Set([1]);
  const reads=[],queries=[];
  let keys=[key(1),key(2)];
  const reader={forge:"fixture",listPullRequests:async query=>{queries.push(query);return {items:query.state==="open"?[]:keys};},getPullRequest:async target=>{
    reads.push(target.number);
    if(unavailable.has(target.number))throw new ForgeNotFound("404 missing");
    return {...target,state:"merged",title:"Done",url:"https://fixture.invalid/pr"};
  }};
  const scan=query=>scanPullRequests(reader,{scope:account,...query},now,operation=>operation(),"connection");
  return {reader,reads,queries,scan,setFailures:values=>unavailable=new Set(values),setKeys:values=>keys=values};
}

test("first 404 never starves the second merge; failures survive restart without blocking enumeration",async()=>{
  const f=scanFixture();let batch=await f.scan({});
  const before=batch.cursor;
  batch=await f.scan({cursor:before});
  assert.deepEqual(batch.items.map(item=>item.number),[2]);assert.equal(batch.pending,false);assert.equal(batch.complete,false);
  assert.equal(batch.issues[0].kind,"not_found");assert.deepEqual(batch.issues[0].key,key(1));
  const cursor=JSON.parse(JSON.stringify(batch.cursor));
  f.setKeys([]);f.setFailures([]);
  const recreated={...f.reader};
  batch=await scanPullRequests(recreated,{scope:account,cursor},now,operation=>operation(),"connection");
  assert.deepEqual(batch.items.map(item=>item.number),[1]);assert.equal(batch.complete,true);assert.deepEqual(batch.issues,[]);
  // Replaying an uncommitted cursor repeats usable facts; it never relies on hidden reader state.
  f.setKeys([key(1),key(2)]);
  const replay=await f.scan({cursor:before});assert.deepEqual(replay.items.map(item=>item.number),[1,2]);
});

test("persisted retries rotate fairly and stay bounded while tracked merges continue",async()=>{
  const f=scanFixture();f.setKeys([1,2,3,4,5,6].map(key));f.setFailures([1,2,3,4,5,6]);
  let batch=await f.scan({});
  for(let i=0;i<3;i++)batch=await f.scan({cursor:batch.cursor,tracked:[key(99)]});
  f.setKeys([]);f.reads.length=0;const seen=new Set();
  for(let i=0;i<4;i++) {
    const start=f.reads.length;
    batch=await f.scan({cursor:batch.cursor,tracked:[key(99)]});
    const reads=f.reads.slice(start);assert(reads.length<=3);
    reads.filter(n=>n!==99).forEach(n=>seen.add(n));
    assert(batch.items.some(item=>item.number===99));assert.equal(batch.complete,false);
  }
  assert.equal(seen.size,6);
});

test("discovery gaps persist without blocking a later window or known merges",async()=>{
  const f=scanFixture();let fail=true;
  f.reader.listPullRequests=async query=>{
    if(query.state==="open" && fail)throw new ForgeRateLimitError("open inventory limited",123);
    return {items:query.state==="all"?[key(2)]:[]};
  };
  let batch=await f.scan({tracked:[key(3)]});assert(batch.items.some(item=>item.number===3));
  batch=await f.scan({cursor:batch.cursor});assert(batch.items.some(item=>item.number===2));
  assert.equal(batch.pending,false);assert.equal(batch.complete,false);
  assert.equal(batch.issues[0].retryAfterMs,123);assert(batch.issues[0].window);
  fail=false;batch=await f.scan({cursor:batch.cursor});assert.equal(batch.complete,true);
});

test("scan cursors reject a different scope, connection or old version before I/O",async()=>{
  const f=scanFixture();const batch=await f.scan({});const count=f.queries.length;
  await assert.rejects(f.scan({cursor:batch.cursor,scope:repoScope}),/scope/);
  await assert.rejects(scanPullRequests(f.reader,{scope:account,cursor:batch.cursor},now,op=>op(),"changed"),/connection/);
  await assert.rejects(f.scan({cursor:JSON.stringify({version:1})}),/start a new scan/);
  assert.equal(f.queries.length,count);
});


test("partial discovery pages return healthy facts but retain coverage gaps until repaired",async()=>{
  const f=scanFixture();let malformed=true;
  f.reader.listPullRequests=async query=>({items:[key(2)],...(malformed?{issues:[{stage:"list",kind:"invalid_data",message:"bad row"}]}:{})});
  let batch=await f.scan({scope:repoScope});
  assert(batch.items.some(item=>item.number===2));assert.equal(batch.complete,false);
  batch=await f.scan({scope:repoScope,cursor:batch.cursor});
  assert.equal(batch.pending,false);assert.equal(batch.complete,false);assert(batch.issues.some(issue=>issue.window));
  malformed=false;batch=await f.scan({scope:repoScope,cursor:batch.cursor});assert.equal(batch.complete,true);
});

test("a full retry backlog pauses new details without dropping their buffered positions",async()=>{
  let next=1,recovered=false;const reads=[];
  const reader={forge:"fixture",listPullRequests:async()=>({items:Array.from({length:5},()=>key(next++))}),getPullRequest:async target=>{
    reads.push(target.number);if(!recovered)throw new ForgeNotFound("404");return {...target,state:"merged"};
  }};
  let cursor,result;
  for(let batch=0;batch<103;batch++) {
    const start=reads.length;
    result=await scanPullRequests(reader,{scope:account,cursor},now,op=>op(),"connection");cursor=result.cursor;
    assert(reads.length-start<=7);
  }
  assert(result.issues.some(issue=>issue.kind==="coverage" && /backlog/.test(issue.message)));
  assert.equal(result.pending,true);assert.equal(result.complete,false);
  const maximum=Math.max(...reads);assert.equal(maximum,500);
  recovered=true;
  result=await scanPullRequests(reader,{scope:account,cursor},now,op=>op(),"connection");
  assert(result.items.some(item=>item.number===501));assert.equal(result.complete,false);
});
