package gpu

import (
	"testing"

	"github.com/wharflab/tally/internal/semantic"
)

func TestStageBaseImageName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		info *semantic.StageInfo
		want string
	}{
		{name: "nil info", info: nil, want: ""},
		{name: "nil base image", info: &semantic.StageInfo{}, want: ""},
		{
			name: "stage ref",
			info: &semantic.StageInfo{
				BaseImage: &semantic.BaseImageRef{Raw: "builder", IsStageRef: true},
			},
			want: "",
		},
		{
			name: "simple image",
			info: &semantic.StageInfo{BaseImage: &semantic.BaseImageRef{Raw: "ubuntu:22.04"}},
			want: "ubuntu",
		},
		{
			name: "nvidia cuda with tag",
			info: &semantic.StageInfo{
				BaseImage: &semantic.BaseImageRef{Raw: "nvidia/cuda:12.2.0-runtime-ubuntu22.04"},
			},
			want: "nvidia/cuda",
		},
		{
			name: "nvidia cuda uppercase",
			info: &semantic.StageInfo{
				BaseImage: &semantic.BaseImageRef{Raw: "NVIDIA/CUDA:12.2.0"},
			},
			want: "nvidia/cuda",
		},
		{
			name: "nvcr registry",
			info: &semantic.StageInfo{
				BaseImage: &semantic.BaseImageRef{Raw: "nvcr.io/nvidia/pytorch:23.10-py3"},
			},
			want: "nvcr.io/nvidia/pytorch",
		},
		{
			name: "digest ref",
			info: &semantic.StageInfo{
				BaseImage: &semantic.BaseImageRef{Raw: "nvidia/cuda@sha256:abc123"},
			},
			want: "nvidia/cuda",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := stageBaseImageName(tt.info)
			if got != tt.want {
				t.Errorf("stageBaseImageName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestStageUsesNVIDIACUDABase(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want bool
	}{
		{name: "nvidia/cuda", raw: "nvidia/cuda:12.2.0-runtime-ubuntu22.04", want: true},
		{name: "docker.io prefix", raw: "docker.io/nvidia/cuda:12.2.0", want: true},
		{name: "nvcr pytorch", raw: "nvcr.io/nvidia/pytorch:23.10-py3", want: false},
		{name: "ubuntu", raw: "ubuntu:22.04", want: false},
		{name: "nvidia cudagl", raw: "nvidia/cudagl:11.4.2-runtime-ubuntu20.04", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			info := &semantic.StageInfo{BaseImage: &semantic.BaseImageRef{Raw: tt.raw}}
			if got := stageUsesNVIDIACUDABase(info); got != tt.want {
				t.Errorf("stageUsesNVIDIACUDABase() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseCUDAImageInfo(t *testing.T) {
	t.Parallel()

	digestRef := "nvidia/cuda@sha256:" +
		"e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

	tests := []struct {
		name       string
		raw        string
		wantCUDA   bool
		wantFlavor cudaFlavor
		wantCuDNN  bool
	}{
		{
			name: "devel tag", raw: "nvidia/cuda:12.2.0-devel-ubuntu22.04",
			wantCUDA: true, wantFlavor: cudaFlavorDevel,
		},
		{
			name: "runtime tag", raw: "nvidia/cuda:12.2.0-runtime-ubuntu22.04",
			wantCUDA: true, wantFlavor: cudaFlavorRuntime,
		},
		{
			name: "base tag", raw: "nvidia/cuda:12.2.0-base-ubuntu22.04",
			wantCUDA: true, wantFlavor: cudaFlavorBase,
		},
		{
			name: "cudnn-devel", raw: "nvidia/cuda:12.2.0-cudnn-devel-ubuntu22.04",
			wantCUDA: true, wantFlavor: cudaFlavorDevel, wantCuDNN: true,
		},
		{
			name: "cudnn-runtime", raw: "nvidia/cuda:12.2.0-cudnn-runtime-ubuntu22.04",
			wantCUDA: true, wantFlavor: cudaFlavorRuntime, wantCuDNN: true,
		},
		{
			name: "no tag defaults to devel", raw: "nvidia/cuda:12.2.0",
			wantCUDA: true, wantFlavor: cudaFlavorDevel,
		},
		{
			name: "docker.io prefix devel", raw: "docker.io/nvidia/cuda:12.2.0-devel-ubuntu22.04",
			wantCUDA: true, wantFlavor: cudaFlavorDevel,
		},
		{
			name: "digest ref defaults to devel", raw: digestRef,
			wantCUDA: true, wantFlavor: cudaFlavorDevel,
		},
		{name: "ubuntu is not CUDA", raw: "ubuntu:22.04", wantCUDA: false},
		{name: "nvcr pytorch is not nvidia/cuda", raw: "nvcr.io/nvidia/pytorch:23.10-py3", wantCUDA: false},
		{name: "nvidia/cudagl is not nvidia/cuda", raw: "nvidia/cudagl:11.4.2-runtime-ubuntu20.04", wantCUDA: false},
		{name: "nil info", raw: "", wantCUDA: false},
		{
			name: "uppercase devel", raw: "NVIDIA/CUDA:12.2.0-devel-ubuntu22.04",
			wantCUDA: true, wantFlavor: cudaFlavorDevel,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var info *semantic.StageInfo
			if tt.raw != "" {
				info = &semantic.StageInfo{BaseImage: &semantic.BaseImageRef{Raw: tt.raw}}
			}
			got := parseCUDAImageInfo(info)
			if got.IsCUDAImage != tt.wantCUDA {
				t.Errorf("IsCUDAImage = %v, want %v", got.IsCUDAImage, tt.wantCUDA)
			}
			if got.IsCUDAImage {
				if got.Flavor != tt.wantFlavor {
					t.Errorf("Flavor = %v, want %v", got.Flavor, tt.wantFlavor)
				}
				if got.HasCuDNN != tt.wantCuDNN {
					t.Errorf("HasCuDNN = %v, want %v", got.HasCuDNN, tt.wantCuDNN)
				}
			}
		})
	}
}

func TestStageUsesNVIDIABase(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want bool
	}{
		{name: "nvidia/cuda", raw: "nvidia/cuda:12.2.0-runtime-ubuntu22.04", want: true},
		{name: "nvidia/cudagl", raw: "nvidia/cudagl:11.4.2-runtime-ubuntu20.04", want: true},
		{name: "nvcr pytorch", raw: "nvcr.io/nvidia/pytorch:23.10-py3", want: true},
		{name: "nvcr triton", raw: "nvcr.io/nvidia/tritonserver:24.01-py3", want: true},
		{name: "docker.io prefix", raw: "docker.io/nvidia/cuda:12.2.0", want: true},
		{name: "ubuntu", raw: "ubuntu:22.04", want: false},
		{name: "pytorch hub", raw: "pytorch/pytorch:2.1.0-cuda12.1-cudnn8-runtime", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			info := &semantic.StageInfo{BaseImage: &semantic.BaseImageRef{Raw: tt.raw}}
			if got := stageUsesNVIDIABase(info); got != tt.want {
				t.Errorf("stageUsesNVIDIABase() = %v, want %v", got, tt.want)
			}
		})
	}
}
