"""Runner boundary tests; mocks here are never labelled native/human E2E."""
import io
import json
import os
from pathlib import Path
import subprocess
import time
import unittest
from unittest.mock import Mock, patch
import urllib.error
import uuid

import verify_social_e2e_local as e


class Response(io.BytesIO):
    def __init__(self, req, raw=b'{"data":[]}', status=200, trace=True):
        super().__init__(raw)
        self.status = status
        self.headers = {"X-Request-ID": req.get_header("X-request-id") if trace else "wrong_trace"}


class Opener:
    def __init__(self, raw=b'{"data":[]}', status=200, error=None, trace=True):
        self.calls = []
        self.raw, self.status, self.error, self.trace = raw, status, error, trace

    def open(self, req, timeout):
        self.calls.append((req, timeout))
        if self.error:
            raise self.error
        return Response(req, self.raw, self.status, self.trace)


class RunnerTests(unittest.TestCase):
    def transport(self, opener, deadline=None):
        self.records = []
        return e.Transport("http://127.0.0.1:48651", deadline or time.monotonic()+10, 123, self.records, opener)

    def runtime(self):
        p = e.WORK / "runner-test-fixtures" / uuid.uuid4().hex
        p.mkdir(parents=True)
        binary = p / "api-test.exe"
        binary.write_bytes(b"LOCAL_TEST_BINARY_NOT_PRODUCT")
        return e.Runtime(binary, e.sha(binary), "birdtie_social_e2e_"+"a"*16, p, time.monotonic()+20)

    def test_explicit_loopback_only(self):
        self.assertEqual(e.local_base("http://127.0.0.1:48001"), "http://127.0.0.1:48001")
        for value in ["https://production.invalid", "http://localhost:48001", "http://127.0.0.1:4173", "http://127.0.0.1:3697", "http://user:password@127.0.0.1:48001", "http://127.0.0.1:48001/path", "http://127.0.0.1:48001?x=1", "http://127.0.0.1:48001#x", "http://127.0.0.1:99999"]:
            with self.subTest(value=value), self.assertRaises(e.Failure):
                e.local_base(value)

    def test_exact_owned_database_and_uuid(self):
        for name in ["birdtie", "postgres", "birdtie_social_e2e_foo", "birdtie_social_e2e_"+"a"*16+"; DROP DATABASE birdtie"]:
            with self.subTest(name=name), self.assertRaises(e.Failure):
                e.owned_database(name)
        self.assertEqual(e.owned_database("birdtie_social_e2e_"+"a"*16), "birdtie_social_e2e_"+"a"*16)
        self.assertEqual(e.identity(str(uuid.uuid4()))[8], "-")
        for value in [None, {}, "https://example.invalid", "uuid'; DELETE"]:
            with self.subTest(value=value), self.assertRaises(e.Failure):
                e.identity(value)

    def test_clean_child_environment_no_borrowed_credential_or_activation(self):
        with patch.dict(os.environ, {"BIRDTIE_OIDC_CLIENT_SECRET":"SECRET", "BIRDTIE_AGENT_FEATURE_FLAGS":"enabled", "BIRDTIE_MODEL_API_KEY":"SECRET", "BIRDTIE_DATABASE_URL":"REMOTE_SECRET", "OPENAI_API_KEY":"SECRET", "KEEP_TEST":"yes"}):
            env = e.clean_environment("birdtie_social_e2e_"+"a"*16, 48001)
        self.assertEqual({k for k in env if k.startswith("BIRDTIE_")}, {"BIRDTIE_DATABASE_URL", "BIRDTIE_API_ADDR", "BIRDTIE_DEV_PHONE_AUTH"})
        self.assertEqual(env["BIRDTIE_API_ADDR"], "127.0.0.1:48001")
        self.assertNotIn("SECRET", json.dumps(env))
        self.assertNotIn("OPENAI_API_KEY", env)
        self.assertNotIn("KEEP_TEST", env)

    def test_trace_credential_body_redaction(self):
        op = Opener()
        http = self.transport(op)
        self.assertEqual(http.request("POST", "/v1/me/profile", {"secretBody":"PRIVATE"}, "TOKEN_SECRET", phase="confirm"), [])
        self.assertEqual(len(op.calls), 1)
        self.assertEqual(op.calls[0][0].get_header("Authorization"), "Bearer TOKEN_SECRET")
        text = json.dumps(self.records)
        for forbidden in ["TOKEN_SECRET", "PRIVATE", "Authorization", "secretBody"]:
            self.assertNotIn(forbidden, text)
        self.assertEqual(self.records[0]["status"], 200)
        self.assertTrue(self.records[0]["requestID"].startswith("e2e_"))

    def test_timeout_mutation_stops_without_retry_or_secret_exception(self):
        for method in ["POST", "PUT", "DELETE"]:
            op = Opener(error=urllib.error.URLError("REMOTE_TOKEN_SECRET"))
            http = self.transport(op)
            with self.subTest(method=method), self.assertRaises(e.UnknownMutation) as caught:
                http.request(method, "/v1/me/conversations", {}, "TOKEN_SECRET")
            self.assertNotIn("TOKEN_SECRET", str(caught.exception))
            self.assertEqual(len(op.calls), 1)
            self.assertEqual(self.records[0]["result"], "UNKNOWN_MUTATION")

    def test_read_failure_not_submission_success(self):
        op = Opener(error=TimeoutError("PRIVATE"))
        with self.assertRaisesRegex(e.Failure, "read_unavailable"):
            self.transport(op).request("GET", "/v1/me/ties")
        self.assertEqual(len(op.calls), 1)
        self.assertEqual(self.records[0]["result"], "READ_UNAVAILABLE")

    def test_trace_mismatch_fail_closed(self):
        with self.assertRaisesRegex(e.Failure, "request_trace_mismatch"):
            self.transport(Opener(trace=False)).request("GET", "/v1/me/ties")

    def test_unexpected_or_redirect_status_no_follow_or_retry(self):
        for status in [302, 403, 500]:
            op = Opener(status=status)
            with self.subTest(status=status), self.assertRaisesRegex(e.Failure, "unexpected_http_status"):
                self.transport(op).request("POST", "/v1/me/conversations", {})
            self.assertEqual(len(op.calls), 1)
        self.assertIsNone(e.NoRedirect().redirect_request(None, None, None, None, None, None))

    def test_expected_acl_failure_has_no_success_data(self):
        self.assertIsNone(self.transport(Opener(b'{"error":{"code":"unauthorized"}}', 401)).request("GET", "/v1/me/ties", expected=(401,)))

    def test_invalid_json_shape_and_bounded_bytes(self):
        for raw in [b"[]", b"null", b"{", b'{"other":1}', b"x"*262145]:
            with self.subTest(length=len(raw)), self.assertRaises(e.Failure):
                self.transport(Opener(raw)).request("GET", "/v1/me/ties")

    def test_deadline_no_request(self):
        op = Opener()
        with self.assertRaisesRegex(e.Failure, "deadline_exceeded"):
            self.transport(op, time.monotonic()-1).request("GET", "/v1/me/ties")
        self.assertEqual(len(op.calls), 0)

    def test_no_query_or_arbitrary_url_transport(self):
        op = Opener()
        for path in ["https://foreign.invalid", "//foreign.invalid", "/readyz", "/v1/me?token=secret", "/v1/me#x", "/v1/me\r\nX-Secret:a"]:
            with self.subTest(path=path), self.assertRaises(e.Failure):
                self.transport(op).request("GET", path)
        self.assertEqual(len(op.calls), 0)

    def test_binary_digest_is_required_before_process(self):
        runtime = self.runtime()
        with self.assertRaisesRegex(e.Failure, "binary_digest_mismatch"):
            e.Runtime(runtime.binary, "0"*64, runtime.database, runtime.out, runtime.deadline)

    def test_sql_rejects_other_database_without_subprocess(self):
        runtime = self.runtime()
        with patch.object(e.subprocess, "run") as run:
            with self.assertRaisesRegex(e.Failure, "unowned_sql_database"):
                runtime.sql("SELECT 1", "birdtie")
            run.assert_not_called()

    def test_refuse_drop_when_marker_not_exact(self):
        runtime = self.runtime()
        runtime.created = True
        runtime.stop = Mock()
        runtime.sql = Mock(return_value="another-owner")
        with self.assertRaisesRegex(e.Failure, "refuse_drop_unowned_database_marker"):
            runtime.cleanup()
        self.assertEqual(runtime.sql.call_count, 1)
        self.assertNotIn("DROP DATABASE", runtime.sql.call_args[0][0])

    def test_refuse_stop_when_child_path_not_same(self):
        runtime = self.runtime()
        runtime.process = Mock(pid=123)
        runtime.process.poll.return_value = None
        runtime.process_identity = Mock(side_effect=e.Failure("owned_api_path_mismatch"))
        with self.assertRaisesRegex(e.Failure, "owned_api_path_mismatch"):
            runtime.stop()
        runtime.process.terminate.assert_not_called()

    def test_refuse_stop_when_listener_not_owned(self):
        runtime = self.runtime()
        runtime.process = Mock(pid=123)
        runtime.process.poll.return_value = None
        runtime.process_identity = Mock()
        runtime.listener_owner = Mock(return_value=False)
        with self.assertRaisesRegex(e.Failure, "refuse_stop_unverified_listener"):
            runtime.stop()
        runtime.process.terminate.assert_not_called()

    def test_cleanup_after_deadline_never_resumes_flow(self):
        runtime = self.runtime()
        runtime.created = True
        runtime.deadline = time.monotonic()-1
        runtime.stop = Mock()
        runtime.sql = Mock(side_effect=[runtime.marker, "", "0"])
        runtime.cleanup()
        self.assertFalse(runtime.created)
        self.assertEqual(runtime.sql.call_count, 3)
        self.assertIn("shobj_description", runtime.sql.call_args_list[0][0][0])
        self.assertIn("DROP DATABASE " + runtime.database, runtime.sql.call_args_list[1][0][0])

    def test_native_trace_exactly_once_same_pid_method_status(self):
        runtime = self.runtime()
        (runtime.out / 'api-process-1.log').write_text('request_id=e2e_testtrace method=POST status=201 latency_ms=2\n')
        processes = [{'pid':123,'generation':1}]
        records = [{'pid':123,'requestID':'e2e_testtrace','method':'POST','status':201}]
        self.assertEqual(e.verify_traces(runtime.out, processes, records)['confirmedRequests'], 1)
        for mutated in [dict(records[0],status=200),dict(records[0],pid=456),dict(records[0],method='GET')]:
            with self.subTest(mutated=mutated), self.assertRaises(e.Failure):
                e.verify_traces(runtime.out, processes, [mutated])
        with self.assertRaisesRegex(e.Failure,'duplicate_request_trace_id'):
            e.verify_traces(runtime.out, processes, records+records)


if __name__ == "__main__":
    unittest.main(verbosity=2)
