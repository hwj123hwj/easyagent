const {test}=require("node:test"),assert=require("node:assert/strict");
const {runRecovery}=require("../.test-output/src/client/run-recovery.js");
test("errors provide authentication, rate-limit and connection actions without auto replay",()=>{assert.equal(runRecovery("服务认证失败，请检查连接令牌").settings,"connections");assert.equal(runRecovery("HTTP 401: invalid api key").settings,"models");assert.equal(runRecovery("HTTP 429 rate limit").kind,"rate_limit");assert.equal(runRecovery("ECONNRESET").kind,"network");assert.equal(runRecovery("bad result").kind,"other")});
