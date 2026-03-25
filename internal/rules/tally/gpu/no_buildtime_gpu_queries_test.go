package gpu

import (
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"

	"github.com/wharflab/tally/internal/testutil"
)

func TestNoBuildtimeGPUQueriesRule_Metadata(t *testing.T) {
	t.Parallel()
	snaps.MatchStandaloneJSON(t, NewNoBuildtimeGPUQueriesRule().Metadata())
}

func TestNoBuildtimeGPUQueriesRule_Check(t *testing.T) {
	t.Parallel()

	testutil.RunRuleTests(t, NewNoBuildtimeGPUQueriesRule(), []testutil.RuleTestCase{
		{
			Name: "nvidia-smi in simple RUN",
			Content: `FROM nvidia/cuda:12.2.0-devel-ubuntu22.04
RUN nvidia-smi
`,
			WantViolations: 1,
			WantCodes:      []string{NoBuildtimeGPUQueriesRuleCode},
			WantMessages:   []string{"nvidia-smi"},
		},
		{
			Name: "nvidia-smi in pipeline",
			Content: `FROM ubuntu:22.04
RUN nvidia-smi | grep "CUDA"
`,
			WantViolations: 1,
			WantMessages:   []string{"nvidia-smi"},
		},
		{
			Name: "nvidia-smi in chain",
			Content: `FROM ubuntu:22.04
RUN apt-get update && nvidia-smi && echo "done"
`,
			WantViolations: 1,
			WantMessages:   []string{"nvidia-smi"},
		},
		{
			Name: "python -c torch.cuda.is_available",
			Content: `FROM nvidia/cuda:12.2.0-devel-ubuntu22.04
RUN python -c "import torch; print(torch.cuda.is_available())"
`,
			WantViolations: 1,
			WantMessages:   []string{"torch.cuda.is_available()"},
		},
		{
			Name: "python3 -c torch.cuda.device_count",
			Content: `FROM nvidia/cuda:12.2.0-devel-ubuntu22.04
RUN python3 -c "print(torch.cuda.device_count())"
`,
			WantViolations: 1,
			WantMessages:   []string{"torch.cuda.device_count()"},
		},
		{
			Name: "torch.cuda.is_available in heredoc python script",
			Content: `FROM nvidia/cuda:12.2.0-devel-ubuntu22.04
RUN <<EOF
python3 -c "
import torch
print(torch.cuda.is_available())
"
EOF
`,
			WantViolations: 1,
			WantMessages:   []string{"torch.cuda.is_available()"},
		},
		{
			Name: "tf.test.is_gpu_available",
			Content: `FROM tensorflow/tensorflow:2.14.0-gpu
RUN python -c "import tensorflow as tf; print(tf.test.is_gpu_available())"
`,
			WantViolations: 1,
			WantMessages:   []string{"tf.test.is_gpu_available()"},
		},
		{
			Name: "tf.config.list_physical_devices",
			Content: `FROM tensorflow/tensorflow:2.14.0-gpu
RUN python -c "import tensorflow as tf; print(tf.config.list_physical_devices('GPU'))"
`,
			WantViolations: 1,
			WantMessages:   []string{"tf.config.list_physical_devices()"},
		},
		{
			Name: "multiple GPU queries in one RUN",
			Content: `FROM nvidia/cuda:12.2.0-devel-ubuntu22.04
RUN nvidia-smi && python -c "torch.cuda.is_available()"
`,
			WantViolations: 1,
			WantMessages:   []string{"nvidia-smi, torch.cuda.is_available()"},
		},
		{
			Name: "nvidia-smi in CMD no violation",
			Content: `FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04
CMD ["nvidia-smi"]
`,
			WantViolations: 0,
		},
		{
			Name: "nvidia-smi in ENTRYPOINT no violation",
			Content: `FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04
ENTRYPOINT ["nvidia-smi"]
`,
			WantViolations: 0,
		},
		{
			Name: "normal RUN no violation",
			Content: `FROM ubuntu:22.04
RUN apt-get update && apt-get install -y python3
`,
			WantViolations: 0,
		},
		{
			Name: "multi-stage only flags offending stage",
			Content: `FROM ubuntu:22.04 AS base
RUN apt-get update

FROM nvidia/cuda:12.2.0-devel-ubuntu22.04 AS builder
RUN nvidia-smi
`,
			WantViolations: 1,
			WantMessages:   []string{"nvidia-smi"},
		},
		{
			Name: "continuation lines",
			Content: `FROM nvidia/cuda:12.2.0-devel-ubuntu22.04
RUN echo "checking GPU" && \
    nvidia-smi && \
    echo "done"
`,
			WantViolations: 1,
			WantMessages:   []string{"nvidia-smi"},
		},
		{
			Name: "heredoc RUN with nvidia-smi",
			Content: `FROM nvidia/cuda:12.2.0-devel-ubuntu22.04
RUN <<EOF
echo "checking GPU"
nvidia-smi
echo "done"
EOF
`,
			WantViolations: 1,
			WantMessages:   []string{"nvidia-smi"},
		},
		{
			Name: "empty scratch no violation",
			Content: `FROM scratch
`,
			WantViolations: 0,
		},
		{
			Name: "nvidia-debugdump in RUN",
			Content: `FROM nvidia/cuda:12.2.0-devel-ubuntu22.04
RUN nvidia-debugdump --list
`,
			WantViolations: 1,
			WantMessages:   []string{"nvidia-debugdump"},
		},
		{
			Name: "torch.cuda.get_device_name",
			Content: `FROM nvidia/cuda:12.2.0-devel-ubuntu22.04
RUN python3 -c "import torch; print(torch.cuda.get_device_name(0))"
`,
			WantViolations: 1,
			WantMessages:   []string{"torch.cuda.get_device_name()"},
		},
		{
			Name: "torch.cuda.current_device",
			Content: `FROM nvidia/cuda:12.2.0-devel-ubuntu22.04
RUN python3 -c "import torch; print(torch.cuda.current_device())"
`,
			WantViolations: 1,
			WantMessages:   []string{"torch.cuda.current_device()"},
		},
	})
}
