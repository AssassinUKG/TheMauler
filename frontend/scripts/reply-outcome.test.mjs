import test from 'node:test'
import assert from 'node:assert/strict'
import { replyOutcomeNotice } from '../src/replyOutcome.ts'

test('completed/recovered answers are not labelled partial', () => {
  assert.equal(replyOutcomeNotice('done'), null)
  assert.equal(replyOutcomeNotice('recovered', 'loop_circuit_breaker'), null)
})
test('output caps and user cancellation have distinct visible outcomes', () => {
  assert.equal(replyOutcomeNotice('stopped', 'output_limit').label, 'Response limit reached')
  assert.equal(replyOutcomeNotice('stopped', 'user_stopped').label, 'Stopped by you')
  assert.match(replyOutcomeNotice('stopped', 'output_limit').detail, /partial/)
  assert.equal(replyOutcomeNotice('done', 'output_limit').label, 'Response limit reached')
})
test('errors and other stops never look completed', () => {
  assert.equal(replyOutcomeNotice('error', 'recovery_persistence_failed').label, 'Response interrupted')
  assert.match(replyOutcomeNotice('stopped', 'review_incomplete').detail, /not a confirmed completion/)
})
