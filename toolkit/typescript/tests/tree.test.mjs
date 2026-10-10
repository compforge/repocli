import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdtemp, mkdir, writeFile, readFile, symlink, rm, realpath } from "node:fs/promises";
import { execFileSync } from "node:child_process";
import { tmpdir } from "node:os";
import { join, dirname } from "node:path";
import { tree, inspect, snapshot, owner, findCheckout } from "../dist/index.js";

function git(root, ...args) { return execFileSync("git", ["-C", root, "-c", "user.name=Test", "-c", "user.email=test@example.com", "-c", "commit.gpgsign=false", ...args], { encoding: "utf8", stdio: ["pipe", "pipe", "pipe"] }).trim(); }
async function write(root, files) { for (const [path, text] of Object.entries(files)) { await mkdir(dirname(join(root,path)),{recursive:true});await writeFile(join(root,path),text); } }
async function repo(t) { const root=await realpath(await mkdtemp(join(tmpdir(),"repocli-tree-")));t.after(()=>rm(root,{recursive:true,force:true}));git(root,"init","-q");return root; }
function commit(root) {git(root,"add",".");git(root,"commit","-qm","fixture");}

test("shared tree contract across versions and independent configuration", async t => {
 const root=await repo(t); const fixture=JSON.parse(await readFile(new URL("../../../conformance/tree/entries.json",import.meta.url),"utf8"));
 await write(root,fixture.files);for(const [path,target]of Object.entries(fixture.symlinks))await symlink(target,join(root,path));commit(root);
 const oid=git(root,"rev-parse","HEAD");git(root,"update-index","--add","--cacheinfo",`160000,${oid},child`);git(root,"commit","-qm","link");
 for(const options of [{},{staged:true},{head:"HEAD"}]) {
  const report=await tree({repository:root,...options});assert.equal(report.complete,true);
  assert.deepEqual(report.directories.map(d=>d.path),fixture.directories);
  assert.deepEqual(Object.fromEntries(report.files.filter(f=>f.role).map(f=>[f.path,f.role])),fixture.roles);
  assert.equal(report.files.find(f=>f.path==="child").oid,oid);
  assert.equal(report.files.find(f=>f.path==="alias").kind,"symlink");
  assert.equal(Boolean(report.files.find(f=>f.path==="go.mod").oid),Object.keys(options).length>0);
  const layout=await inspect({repository:root,...options});assert.deepEqual(layout.components.map(c=>c.root),fixture.components);assert.equal(owner(layout,"docs").root,".");
 }
 await write(root,{".repocli.json":"{"});assert.equal((await tree({repository:root})).complete,true);assert.equal((await inspect({repository:root})).complete,true);
});

test("literal paths, docs-only repository, absence versus corrupt metadata",async t=>{
 const parent=await repo(t);const root=join(parent,"repo \n");await mkdir(root);git(root,"init","-q");await write(root,{"docs/guide.md":"hello"});
 assert.deepEqual((await inspect({repository:root})).components,[]);
 assert.equal((await tree({repository:root})).checkout,root);assert.equal((await snapshot(root)).checkout,root);assert.equal(findCheckout(join(root,"docs")),root);
 await writeFile(join(root,".git/HEAD"),"broken");assert.throws(()=>findCheckout(root));
});

test("gitlink pointer survives without checkout and excludes child contents",async t=>{
 const root=await repo(t);await write(root,{"README":"parent"});commit(root);const child=join(root,"child");await mkdir(child);git(child,"init","-q");await write(child,{"README":"child"});commit(child);
 const oid=git(child,"rev-parse","HEAD");git(root,"update-index","--add","--cacheinfo",`160000,${oid},child`);
 const before=await snapshot(root);await write(child,{"README":"dirty","new":"new"});assert.equal((await snapshot(root)).digest,before.digest);
 commit(child);assert.notEqual((await snapshot(root)).digest,before.digest);await rm(child,{recursive:true});assert.deepEqual(await snapshot(root),before);
});
