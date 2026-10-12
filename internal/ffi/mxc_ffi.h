// Hand-maintained declarations for the subset of mxc_ffi (microsoft/mxc,
// src/ffi/mxc_ffi) used by this binding. The upstream C ABI is explicitly
// unstable: re-diff against src/ffi/mxc_ffi/src/*.rs when bumping MXC.
#ifndef MXC_FFI_H
#define MXC_FFI_H
#include <stdint.h>
#include <stddef.h>

typedef struct MxcSandbox MxcSandbox;
typedef struct MxcReadStream MxcReadStream;
typedef struct MxcWriteStream MxcWriteStream;
typedef struct MxcStreamCloser MxcStreamCloser;

typedef struct {
  char *message_utf8;
  char *operation_utf8;
  char *native_code_utf8;
  char *remediation_utf8;
} MxcErrorDetail;

typedef struct {
  int32_t status;
  int32_t exit_code;
  int32_t timed_out;
  char *stdout_utf8;
  char *stderr_utf8;
  MxcErrorDetail error;
  char *output_metadata_json_utf8;
  char *warnings_json_utf8;
} MxcRunResult;

typedef struct {
  int32_t status;
  char *response_json_utf8;
  MxcErrorDetail error;
} MxcStateAwareResult;

const char *mxc_version(void);
char *mxc_available_backends_json(void);
char *mxc_platform_support_json(void);
void mxc_string_free(char *s);

int32_t mxc_run_json(const char *req, int32_t experimental, MxcRunResult *out);
void mxc_run_result_free(MxcRunResult *r);
void mxc_error_detail_free(MxcErrorDetail *d);

int32_t mxc_spawn_json(const char *req, int32_t experimental, MxcSandbox **out_handle, MxcErrorDetail *out_error);
int32_t mxc_spawn_pty_json(const char *req, int32_t experimental, uint16_t rows, uint16_t cols, MxcSandbox **out_handle, MxcErrorDetail *out_error);
int32_t mxc_sandbox_pty_resize(MxcSandbox *h, uint16_t rows, uint16_t cols);

MxcWriteStream *mxc_sandbox_take_stdin(MxcSandbox *h);
MxcReadStream *mxc_sandbox_take_stdout(MxcSandbox *h);
MxcReadStream *mxc_sandbox_take_stderr(MxcSandbox *h);
MxcStreamCloser *mxc_sandbox_stdout_closer(MxcSandbox *h);
MxcStreamCloser *mxc_sandbox_stderr_closer(MxcSandbox *h);
int32_t mxc_stream_read(MxcReadStream *s, uint8_t *buf, size_t cap, size_t *out_read);
int32_t mxc_stream_write(MxcWriteStream *s, const uint8_t *buf, size_t len, size_t *out_written);
int32_t mxc_stream_flush(MxcWriteStream *s);
int32_t mxc_stream_closer_close(MxcStreamCloser *c);
void mxc_read_stream_free(MxcReadStream *s);
void mxc_write_stream_free(MxcWriteStream *s);
void mxc_stream_closer_free(MxcStreamCloser *c);

uint32_t mxc_sandbox_id(MxcSandbox *h);
int32_t mxc_sandbox_output_metadata_json(MxcSandbox *h, char **out);
int32_t mxc_sandbox_warnings_json(MxcSandbox *h, char **out);
int32_t mxc_sandbox_try_wait(MxcSandbox *h, int32_t *exit, int32_t *running, int32_t *timed_out);
int32_t mxc_sandbox_wait(MxcSandbox *h, int32_t *exit, int32_t *timed_out);
int32_t mxc_sandbox_kill(MxcSandbox *h);
int32_t mxc_sandbox_kill_for_timeout(MxcSandbox *h);
void mxc_sandbox_free(MxcSandbox *h);

int32_t mxc_run_state_aware_json(const char *req, int32_t dry_run, int32_t experimental, MxcStateAwareResult *out);
void mxc_state_aware_result_free(MxcStateAwareResult *r);
int32_t mxc_exec_state_aware_json(const char *req, int32_t experimental, MxcSandbox **out_handle, MxcErrorDetail *out_error);
int32_t mxc_run_state_aware_exec_json(const char *req, int32_t experimental, MxcRunResult *out);
int32_t mxc_state_aware_exec_pty(const char *req, int32_t experimental, uint16_t rows, uint16_t cols, MxcSandbox **out_handle, MxcErrorDetail *out_error);

int32_t mxc_telemetry_get_consent(char **out);
int32_t mxc_telemetry_get_consent_status(char **out);
int32_t mxc_telemetry_withdraw_consent(char **out);
int32_t mxc_telemetry_get_policy(char **out);
int32_t mxc_telemetry_needs_consent_prompt(int32_t *out);
#endif
