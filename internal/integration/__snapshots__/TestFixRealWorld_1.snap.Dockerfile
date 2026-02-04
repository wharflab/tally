ARG BUILDER_IMAGE=ubuntu:22.04
ARG RUNTIME_IMAGE=ubuntu:22.04
FROM $BUILDER_IMAGE AS python_builder_1

RUN <<EOF
set -e
apt-get update
apt-get install -y --no-install-recommends curl ca-certificates wget
apt-get clean
rm -rf /var/lib/apt/lists/*
EOF

ARG MAMBA_VERSION
RUN <<EOF
set -e
curl -L -o ~/mambaforge.sh https://github.com/conda-forge/miniforge/releases/download/${MAMBA_VERSION}/Mambaforge-${MAMBA_VERSION}-Linux-x86_64.sh
chmod +x ~/mambaforge.sh
~/mambaforge.sh -b -p /opt/conda
rm ~/mambaforge.sh
EOF

ARG PYTHON_VERSION
RUN <<EOF
set -e
/opt/conda/bin/conda config --set auto_activate_base false
/opt/conda/bin/conda create --name default python=${PYTHON_VERSION}
echo "#! /bin/bash\n\n# script to activate the conda environment" >~/.bashrc
echo "export PS1='Docker> '" >>~/.bashrc
/opt/conda/bin/conda init bash
echo "\nconda activate default" >>~/.bashrc
/opt/conda/bin/conda clean -a
EOF

ENV BASH_ENV ~/.bashrc
ENV PATH="${PATH}:/opt/conda/envs/default/bin"

RUN <<EOF
set -e
ln -s /opt/conda/envs/default/bin/pip /usr/local/bin/pip
ln -s /opt/conda/envs/default/bin/python /usr/local/bin/python
/opt/conda/bin/conda config --set ssl_verify False
pip install --no-cache-dir --upgrade pip --no-cache-dir --trusted-host pypi.org --trusted-host files.pythonhosted.org
pip install --no-cache-dir pyOpenSSL --upgrade
EOF

ARG DEBIAN_FRONTEND=noninteractive

#ARG PYTHON_VERSION
#RUN /opt/conda/bin/conda remove -y python=3.10 && /opt/conda/bin/conda install -y python=$PYTHON_VERSION && /opt/conda/bin/conda clean -ya

RUN <<EOF
set -e
apt-get update
apt-get install -y libopenmpi-dev
rm -rf /var/lib/apt/lists/*
apt-get clean
pip install --no-cache-dir -U "cython<3.0.0" wheel
pip install pyyaml==5.4.1 --no-build-isolation
pip install --no-cache-dir -U "awscli>1.27,<2" boto3 "click==8.1.2,<9" "cmake>=3.24.3,<3.25" "cryptography>41" ipython "mpi4py>=3.1.4,<3.2" "opencv-python>=4.6.0,<4.7" packaging Pillow "psutil>=5.9.4,<5.10" "pyyaml>=5.4,<5.5"
EOF

ARG TRITON_VERSION
RUN <<EOF
set -e
pip install --no-cache-dir -U smclarify "sagemaker>=2,<3" sagemaker-experiments==0.* sagemaker-pytorch-training triton==${TRITON_VERSION}
pip install --no-cache-dir -U "bokeh>=3.0.1,<4" "imageio>=2.22,<3" "opencv-python>=4.6,<5" "plotly>=5.11,<6" "seaborn>=0.12,<1" "numba>=0.56.4,<0.57" "shap>=0.41,<1"
apt-get update
apt-get install -y build-essential
rm -rf /var/lib/apt/lists/*
apt-get clean
EOF

ARG DATASETS_VERSION
ARG DIFFUSERS_VERSION
ARG TRANSFORMERS_VERSION
RUN pip install --no-cache-dir kenlm==0.1 \
                               transformers[sklearn,sentencepiece,audio,vision]==${TRANSFORMERS_VERSION} \
                               datasets==${DATASETS_VERSION} \
                               diffusers==${DIFFUSERS_VERSION} \
                               $PT_TORCHAUDIO_URL \
                               multiprocess==0.70.14 \
                               dill==0.3.6 \
                               sagemaker==2.132.0 \
                               evaluate \
                               gevent~=23.9.0 \
                               pyarrow~=14.0.1
RUN pip install --no-cache-dir setuptools==69.5.1

COPY requirements1.txt .
RUN pip install --no-cache-dir -r requirements1.txt

ARG SMD_MODEL_PARALLEL_URL
RUN pip install --no-cache-dir -U ${SMD_MODEL_PARALLEL_URL}

ARG SMD_DATA_PARALLEL_URL
RUN pip install --no-cache-dir ${SMD_DATA_PARALLEL_URL}

FROM $RUNTIME_IMAGE AS runtime

ARG TARGETARCH

ARG EFA_PATH=/opt/amazon/efa

ARG DEBIAN_FRONTEND=noninteractive

ENV SAGEMAKER_TRAINING_MODULE=sagemaker_pytorch_container.training:main
ARG PYTHON_SHORT_VERSION
ARG PYTHON_VERSION
ARG PYTHON

ARG PYTHON
ARG HOME_DIR

ENV NVIDIA_REQUIRE_CUDA=cuda>=11.7 brand=tesla,driver>=450,driver<451 brand=tesla,driver>=470,driver<471 brand=unknown,driver>=470,driver<471 brand=nvidia,driver>=470,driver<471 brand=nvidiartx,driver>=470,driver<471 brand=geforce,driver>=470,driver<471 brand=geforcertx,driver>=470,driver<471 brand=quadro,driver>=470,driver<471 brand=quadrortx,driver>=470,driver<471 brand=titan,driver>=470,driver<471 brand=titanrtx,driver>=470,driver<471 brand=tesla,driver>=510,driver<511 brand=unknown,driver>=510,driver<511 brand=nvidia,driver>=510,driver<511 brand=nvidiartx,driver>=510,driver<511 brand=geforce,driver>=510,driver<511 brand=geforcertx,driver>=510,driver<511 brand=quadro,driver>=510,driver<511 brand=quadrortx,driver>=510,driver<511 brand=titan,driver>=510,driver<511 brand=titanrtx,driver>=510,driver<511

ENV PYTHONDONTWRITEBYTECODE=1
ENV PYTHONUNBUFFERED=1

ENV LD_LIBRARY_PATH=/opt/conda/lib:/usr/local/lib:/usr/local/nvidia/lib:/usr/local/nvidia/lib64

ENV PATH="${PATH}:/opt/conda/bin:/usr/local/nvidia/bin:/usr/local/cuda/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"

ENV TORCH_CUDA_ARCH_LIST="3.7 5.0 7.0+PTX 8.0"

ENV TORCH_NVCC_FLAGS="-Xfatbin -compress-all"

ENV CUDNN_VERSION=8.5.0.96
ENV NCCL_VERSION=2.14.3
ENV HOROVOD_VERSION=0.26.1
ENV EFA_VERSION=1.19.0
ENV OMPI_VERSION=4.1.1
ENV BRANCH_OFI=1.4.0-aws
ENV GDRCOPY_VERSION=2.3.1

ENV CMAKE_PREFIX_PATH="$(dirname $(which conda))/../"
ENV OPEN_MPI_PATH=/opt/amazon/openmpi
ENV DGLBACKEND=pytorch
ENV MANUAL_BUILD=0
ENV RDMAV_FORK_SAFE=1
ENV DLC_CONTAINER_TYPE=training

LABEL org.opencontainers.image.ref.name=ubuntu
LABEL org.opencontainers.image.version=22.04

#CMD ["/bin/bash"]

RUN <<EOF
set -e
apt-get update
apt-get install -y wget build-essential zlib1g-dev libncurses5-dev libgdbm-dev libnss3-dev libssl-dev libreadline-dev libffi-dev libsqlite3-dev libbz2-dev liblzma-dev
apt-get clean
rm -rf /var/lib/apt/lists/*
EOF

#RUN cd /tmp \
#    && wget https://www.python.org/ftp/python/${PYTHON_VERSION}/Python-${PYTHON_VERSION}.tgz \
#    && tar -xvf Python-${PYTHON_VERSION}.tgz \
#    && cd Python-${PYTHON_VERSION} \
#    && ./configure --enable-optimizations \
#    && make -j8 && make install && cd .. && rm Python-${PYTHON_VERSION}.tgz && rm -r Python-${PYTHON_VERSION} && ln -s /usr/local/bin/python3 /usr/local/bin/python && ln -s /usr/local/bin/pip3 /usr/local/bin/pip && python -m pip install --upgrade pip && rm -r /root/.cache/pip

WORKDIR /app

ENV NVARCH=x86_64
RUN <<EOF
set -e
apt-get update
apt-get install -y --no-install-recommends gnupg2 curl ca-certificates
curl -fsSL https://developer.download.nvidia.com/compute/cuda/repos/ubuntu2204/${NVARCH}/3bf863cc.pub | apt-key add -
EOF
COPY <<EOF /etc/apt/sources.list.d/cuda.list
deb https://developer.download.nvidia.com/compute/cuda/repos/ubuntu2204/${NVARCH} /
EOF
RUN apt-get purge --autoremove -y curl && rm -rf /var/lib/apt/lists/*

ENV NV_CUDA_COMPAT_PACKAGE=cuda-compat-11-7
ENV NV_CUDA_CUDART_VERSION=11.7.99-1
RUN <<EOF
set -e
apt-get update
apt-get install -y --no-install-recommends cuda-cudart-11-7=${NV_CUDA_CUDART_VERSION} ${NV_CUDA_COMPAT_PACKAGE}
rm -rf /var/lib/apt/lists/*
echo "/usr/local/nvidia/lib" >>/etc/ld.so.conf.d/nvidia.conf
echo "/usr/local/nvidia/lib64" >>/etc/ld.so.conf.d/nvidia.conf
EOF

ENV NVIDIA_VISIBLE_DEVICES=all
ENV NVIDIA_DRIVER_CAPABILITIES=compute,utility
ENV DEBIAN_FRONTEND=noninteractive LD_LIBRARY_PATH=/usr/local/nvidia/lib:/usr/local/nvidia/lib64:/usr/local/lib

RUN <<EOF
set -e
apt-get update
apt-get upgrade -y
apt-get autoremove -y
apt-get clean
rm -rf /var/lib/apt/lists/*
EOF

ARG CUBLAS_VERSION=11.10.3.66
RUN <<EOF
set -e
apt-get update
apt-get -y upgrade --only-upgrade systemd
apt-get install -y --allow-change-held-packages --no-install-recommends build-essential ca-certificates cmake cuda-command-line-tools-11-7 cuda-cudart-11-7 cuda-libraries-11-7 curl emacs git hwloc jq libcublas-11-7=${CUBLAS_VERSION}-1 libcublas-dev-11-7=${CUBLAS_VERSION}-1 libcudnn8=$CUDNN_VERSION-1+cuda11.7 libcufft-dev-11-7 libcurand-dev-11-7 libcurl4-openssl-dev libcusolver-dev-11-7 libcusparse-dev-11-7 libglib2.0-0 libgl1-mesa-glx libsm6 libxext6 libxrender-dev libgomp1 libibverbs-dev libhwloc-dev libnuma1 libnuma-dev libssl3 libssl-dev libtool openssl python3-dev unzip vim wget zlib1g-dev pkg-config check libsubunit0 libsubunit-dev
rm -rf /var/lib/apt/lists/*
apt-get clean
cd /tmp
git clone https://github.com/NVIDIA/nccl.git -b v${NCCL_VERSION}-1
cd nccl
make -j $(nproc) src.build BUILDDIR=/usr/local
rm -rf /tmp/nccl
mkdir /tmp/efa
cd /tmp/efa
curl -O https://efa-installer.amazonaws.com/aws-efa-installer-${EFA_VERSION}.tar.gz
tar -xf aws-efa-installer-${EFA_VERSION}.tar.gz
cd aws-efa-installer
apt-get update
./efa_installer.sh -y --skip-kmod -g
rm -rf $OPEN_MPI_PATH
rm -rf /tmp/efa
rm -rf /tmp/aws-efa-installer-${EFA_VERSION}.tar.gz
rm -rf /var/lib/apt/lists/*
apt-get clean
mkdir /tmp/openmpi
cd /tmp/openmpi
wget --quiet https://download.open-mpi.org/release/open-mpi/v4.1/openmpi-${OMPI_VERSION}.tar.gz
tar zxf openmpi-${OMPI_VERSION}.tar.gz
cd openmpi-${OMPI_VERSION}
./configure --enable-orterun-prefix-by-default --prefix=$OPEN_MPI_PATH --with-cuda
make -j $(nproc) all
make install
ldconfig
cd /
rm -rf /tmp/openmpi
EOF

 ENV PATH="${PATH}:/opt/amazon/openmpi/bin:/opt/amazon/efa/bin:/opt/conda/bin:/usr/local/nvidia/bin:/usr/local/cuda/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
 ENV LD_LIBRARY_PATH="${LD_LIBRARY_PATH}/opt/amazon/openmpi/lib/:/opt/amazon/efa/lib/"

RUN <<EOF
set -e
cd /tmp
git clone https://github.com/NVIDIA/gdrcopy.git -b v${GDRCOPY_VERSION}
cd gdrcopy
sed -ie '12s@$@ -L /usr/local/cuda/lib64/stubs/@' tests/Makefile
make install
rm -rf /tmp/gdrcopy
EOF

ENV REQUESTS_CA_BUNDLE=/etc/ssl/certs/ca-certificates.crt

RUN <<EOF
set -e
apt-get update -q
apt-get install -q -y --no-install-recommends bzip2 ca-certificates git libglib2.0-0 libsm6 libxext6 libxrender1 mercurial openssh-client procps subversion wget
apt-get clean
rm -rf /var/lib/apt/lists/*
EOF

#ARG CONDA_VERSION=py39_22.11.1-1
#RUN set -x &&     UNAME_M="$(uname -m)" &&     if [ "${UNAME_M}" = "x86_64" ]; then         MINICONDA_URL="https://repo.anaconda.com/miniconda/Miniconda3-${CONDA_VERSION}-Linux-x86_64.sh";         SHA256SUM="e685005710679914a909bfb9c52183b3ccc56ad7bb84acc861d596fcbe5d28bb";     elif [ "${UNAME_M}" = "s390x" ]; then         MINICONDA_URL="https://repo.anaconda.com/miniconda/Miniconda3-${CONDA_VERSION}-Linux-s390x.sh";         SHA256SUM="a150511e7fd19d07b770f278fb5dd2df4bc24a8f55f06d6274774f209a36c766";     elif [ "${UNAME_M}" = "aarch64" ]; then         MINICONDA_URL="https://repo.anaconda.com/miniconda/Miniconda3-${CONDA_VERSION}-Linux-aarch64.sh";         SHA256SUM="48a96df9ff56f7421b6dd7f9f71d548023847ba918c3826059918c08326c2017";     elif [ "${UNAME_M}" = "ppc64le" ]; then         MINICONDA_URL="https://repo.anaconda.com/miniconda/Miniconda3-${CONDA_VERSION}-Linux-ppc64le.sh";         SHA256SUM="4c86c3383bb27b44f7059336c3a46c34922df42824577b93eadecefbf7423836";     fi &&     wget "${MINICONDA_URL}" -O miniconda.sh -q &&     echo "${SHA256SUM} miniconda.sh" > shasum &&     if [ "${CONDA_VERSION}" != "latest" ]; then sha256sum --check --status shasum; fi &&     mkdir -p /opt &&     bash miniconda.sh -b -p /opt/conda &&     rm miniconda.sh shasum &&     ln -s /opt/conda/etc/profile.d/conda.sh /etc/profile.d/conda.sh &&     echo ". /opt/conda/etc/profile.d/conda.sh" >> ~/.bashrc &&     echo "conda activate base" >> ~/.bashrc &&     find /opt/conda/ -follow -type f -name '*.a' -delete &&     find /opt/conda/ -follow -type f -name '*.js.map' -delete &&     /opt/conda/bin/conda clean -afy

ARG MAMBA_VERSION
RUN <<EOF
set -e
curl -L -o ~/mambaforge.sh https://github.com/conda-forge/miniforge/releases/download/${MAMBA_VERSION}/Mambaforge-${MAMBA_VERSION}-Linux-x86_64.sh
chmod +x ~/mambaforge.sh
~/mambaforge.sh -b -p /opt/conda
rm ~/mambaforge.sh
/opt/conda/bin/conda install conda-libmamba-solver --solver classic
/opt/conda/bin/conda config --set solver libmamba
EOF

ARG PYTHON_VERSION
RUN <<EOF
set -e
/opt/conda/bin/conda config --set auto_activate_base false
/opt/conda/bin/conda create --name default python=${PYTHON_VERSION}
echo "#! /bin/bash\n\n# script to activate the conda environment" >~/.bashrc
echo "export PS1='Docker> '" >>~/.bashrc
/opt/conda/bin/conda init bash
echo "\nconda activate default" >>~/.bashrc
/opt/conda/bin/conda clean -a
EOF

ENV BASH_ENV ~/.bashrc
ENV PATH="${PATH}:/opt/conda/envs/default/bin"

#RUN conda config --set auto_activate_base false && conda create --name default python={PYHON_VERSION} && echo "source activate default" >> ~/.bashrc
#RUN "source activate default"

RUN <<EOF
set -e
ln -s /opt/conda/envs/default/bin/pip /usr/local/bin/pip
ln -s /opt/conda/envs/default/bin/python /usr/local/bin/python
/opt/conda/bin/conda config --set ssl_verify False
pip install --no-cache-dir --upgrade pip --no-cache-dir --trusted-host pypi.org --trusted-host files.pythonhosted.org
pip install --no-cache-dir pyOpenSSL --upgrade
/opt/conda/bin/conda install -y -c conda-forge cython mkl mkl-include parso typing h5py requests pyopenssl libgcc conda-content-trust charset-normalizer accelerate
/opt/conda/bin/conda install -c dglteam -y dgl-cuda11.7=0.9.1
/opt/conda/bin/conda install -c pytorch -y magma-cuda117
/opt/conda/bin/conda install -c fastai fastai
pip uninstall -y dataclasses
/opt/conda/bin/conda clean -ya
rm -rf /root/micromamba/
rm -rf /opt/conda/lib/libtinfo.so
EOF

COPY --from=python_builder_1 /opt/conda /opt/conda


ARG PYTORCH_VERSION
ARG PYTORCH_VERSION_SUFFIX
ARG TORCHVISION_VERSION
ARG TORCHVISION_VERSION_SUFFIX
ARG TORCHAUDIO_VERSION
ARG TORCHAUDIO_VERSION_SUFFIX
ARG PYTORCH_DOWNLOAD_URL

#RUN if [ ! $TORCHAUDIO_VERSION ];     then         TORCHAUDIO=;     else         TORCHAUDIO=torchaudio==${TORCHAUDIO_VERSION}${TORCHAUDIO_VERSION_SUFFIX};     fi &&     if [ ! $PYTORCH_DOWNLOAD_URL ];     then         pip install --no-cache-dir -U            torch==${PYTORCH_VERSION}${PYTORCH_VERSION_SUFFIX}             torchvision==${TORCHVISION_VERSION}${TORCHVISION_VERSION_SUFFIX}             ${TORCHAUDIO};     else         pip install --no-cache-dir -U             torch==${PYTORCH_VERSION}${PYTORCH_VERSION_SUFFIX}             torchvision==${TORCHVISION_VERSION}${TORCHVISION_VERSION_SUFFIX}             ${TORCHAUDIO}             -f ${PYTORCH_DOWNLOAD_URL};     fi &&     rm -r /root/.cache/pip

RUN <<EOF
set -e
apt-get update
apt-get install -y git pdsh libaio1 libaio-dev pigz
rm -rf /var/lib/apt/lists/*
apt-get clean
EOF

ARG FLASH_ATTN_VERSION
RUN pip install --no-cache-dir --user flash-attn==${FLASH_ATTN_VERSION}

 WORKDIR /root

 COPY deep_learning_container.py /usr/local/bin/deep_learning_container.py
RUN <<EOF
set -e
chmod +x /usr/local/bin/deep_learning_container.py
curl -o /license.txt https://aws-dlc-licenses.s3.amazonaws.com/pytorch-1.13/license.txt
rm -rf /root/.cache
EOF

ARG PT_TORCHDATA_URL
ARG PT_TORCHAUDIO_URL
ARG PT_TORCHVISION_URL
ARG PT_SM_TRAINING_URL
RUN pip uninstall -y torch torchvision torchaudio torchdata  && pip install --no-cache-dir -U ${PT_SM_TRAINING_URL} ${PT_TORCHVISION_URL} ${PT_TORCHAUDIO_URL} ${PT_TORCHDATA_URL}

ENV LD_LIBRARY_PATH="/usr/local/nvidia/lib:/usr/local/nvidia/lib64:/usr/local/lib:/opt/amazon/openmpi/lib/:/opt/amazon/efa/lib/"

RUN <<EOF
set -e
echo $PATH
echo $LD_LIBRARY_PATH
pip install -U --force-reinstall --no-cache-dir wheel==0.43.0 setuptools==70.1.0
pip install --force-reinstall --no-cache-dir setuptools==69.5.1
git clone https://github.com/NVIDIA/apex
cd apex
git checkout aa756ce
pip install -v --no-cache-dir --global-option="--cpp_ext" --global-option="--cuda_ext" ./
mv $OPEN_MPI_PATH/bin/mpirun $OPEN_MPI_PATH/bin/mpirun.real
echo '#!/bin/bash' >$OPEN_MPI_PATH/bin/mpirun
echo "${OPEN_MPI_PATH}/bin/mpirun.real --allow-run-as-root \"\$@\"" >>$OPEN_MPI_PATH/bin/mpirun
chmod a+x $OPEN_MPI_PATH/bin/mpirun
echo "hwloc_base_binding_policy = none" >>$OPEN_MPI_PATH/etc/openmpi-mca-params.conf
echo "rmaps_base_mapping_policy = slot" >>$OPEN_MPI_PATH/etc/openmpi-mca-params.conf
echo NCCL_DEBUG=INFO >>/etc/nccl.conf
echo NCCL_SOCKET_IFNAME=^docker0 >>/etc/nccl.conf
mkdir /tmp/efa-ofi-nccl
cd /tmp/efa-ofi-nccl
git clone https://github.com/aws/aws-ofi-nccl.git -b v${BRANCH_OFI}
cd aws-ofi-nccl
./autogen.sh
./configure --with-libfabric=/opt/amazon/efa --with-mpi=/opt/amazon/openmpi --with-cuda=/usr/local/cuda --with-nccl=/usr/local --prefix=/usr/local
make
make install
rm -rf /tmp/efa-ofi-nccl
rm -rf /var/lib/apt/lists/*
apt-get clean
apt-get update
apt-get install -y --allow-downgrades --allow-change-held-packages --no-install-recommends
apt-get install -y --no-install-recommends openssh-client openssh-server
mkdir -p /var/run/sshd
cat /etc/ssh/ssh_config | grep -v StrictHostKeyChecking >/etc/ssh/ssh_config.new
echo "    StrictHostKeyChecking no" >>/etc/ssh/ssh_config.new
mv /etc/ssh/ssh_config.new /etc/ssh/ssh_config
rm -rf /var/lib/apt/lists/*
apt-get clean
mkdir -p /var/run/sshd
sed 's@session\s*required\s*pam_loginuid.so@session optional pam_loginuid.so@g' -i /etc/pam.d/sshd
rm -rf /root/.ssh/
mkdir -p /root/.ssh/
ssh-keygen -q -t rsa -N '' -f /root/.ssh/id_rsa
cp /root/.ssh/id_rsa.pub /root/.ssh/authorized_keys
printf "Host *\n StrictHostKeyChecking no\n" >>/root/.ssh/config
EOF

ARG CUDA_HOME=/usr/local/cuda
RUN <<EOF
set -e
pip uninstall -y horovod
ldconfig /usr/local/cuda-11.7/targets/x86_64-linux/lib/stubs
HOROVOD_GPU_ALLREDUCE=NCCL HOROVOD_CUDA_HOME=/usr/local/cuda-11.7 HOROVOD_WITH_PYTORCH=1 pip install --no-cache-dir horovod==${HOROVOD_VERSION}
ldconfig
mkdir -p /etc/pki/tls/certs
cp /etc/ssl/certs/ca-certificates.crt /etc/pki/tls/certs/ca-bundle.crt
conda install -y -c conda-forge scikit-learn pandas
EOF

WORKDIR /

RUN <<EOF
set -e
wget https://sourceforge.net/projects/boost/files/boost/1.73.0/boost_1_73_0.tar.gz/download -O boost_1_73_0.tar.gz && tar -xzf boost_1_73_0.tar.gz && cd boost_1_73_0 && ./bootstrap.sh && ./b2 threading=multi --prefix=/opt/conda -j 64 cxxflags=-fPIC cflags=-fPIC install || true
cd ..
rm -rf boost_1_73_0.tar.gz
rm -rf boost_1_73_0
cd /opt/conda/include/boost
EOF

WORKDIR /opt/

COPY start_with_right_hostname.sh /usr/local/bin/start_with_right_hostname.sh

RUN chmod +x /usr/local/bin/start_with_right_hostname.sh

WORKDIR /root


ARG SMDEBUG_VERSION=1.0.34
RUN <<EOF
set -e
cd /tmp
git clone https://github.com/awslabs/sagemaker-debugger --branch ${SMDEBUG_VERSION} --depth 1 --single-branch
cd sagemaker-debugger
pip install .
rm -rf /tmp/*
rm /etc/apt/sources.list.d/*
git clone https://github.com/KarypisLab/GKlib
cd GKlib
make config
make
make install
cd ..
git clone https://github.com/KarypisLab/METIS.git
cd METIS
make config shared=1 cc=gcc prefix=/root/local
make install
cd ..
rm -rf METIS GKlib
rm -rf /var/lib/apt/lists/*
apt-get clean
EOF

ARG RMM_VERSION=0.15.0
RUN <<EOF
set -e
wget -nv https://github.com/rapidsai/rmm/archive/v${RMM_VERSION}.tar.gz
tar -xvf v${RMM_VERSION}.tar.gz
cd rmm-${RMM_VERSION}
INSTALL_PREFIX=/usr/local ./build.sh librmm
cd ..
rm -rf v${RMM_VERSION}.tar*
rm -rf rmm-${RMM_VERSION}
EOF


ENV LD_LIBRARY_PATH="${LD_LIBRARY_PATH}:/opt/conda/lib/python3.9/site-packages/smdistributed/dataparallel/lib"

RUN <<EOF
set -e
apt-get update
apt-get install -y --allow-change-held-packages --no-install-recommends libunwind-dev
rm -rf /var/lib/apt/lists/*
apt-get clean
EOF

ARG SMPPY_BINARY

RUN <<EOF
set -e
wget -nv https://smppy.s3.amazonaws.com/pytorch/cu117/${SMPPY_BINARY}
pip install ${SMPPY_BINARY}
rm ${SMPPY_BINARY}
EOF

WORKDIR /

RUN <<EOF
set -e
HOME_DIR=/root
curl -o ${HOME_DIR}/oss_compliance.zip https://aws-dlinfra-utilities.s3.amazonaws.com/oss_compliance.zip
unzip ${HOME_DIR}/oss_compliance.zip -d ${HOME_DIR}/
cp ${HOME_DIR}/oss_compliance/test/testOSSCompliance /usr/local/bin/testOSSCompliance
chmod +x /usr/local/bin/testOSSCompliance
chmod +x ${HOME_DIR}/oss_compliance/generate_oss_compliance.sh
${HOME_DIR}/oss_compliance/generate_oss_compliance.sh ${HOME_DIR} ${PYTHON}
rm -rf ${HOME_DIR}/oss_compliance*
rm -rf /tmp/tmp*
rm -rf /root/.cache | true
EOF

ENTRYPOINT ["bash", "-m", "start_with_right_hostname.sh"]

CMD ["/bin/bash"]


RUN <<EOF
set -e
apt-get update
apt-get -y upgrade --only-upgrade systemd openssl cryptsetup
apt-get install -y git-lfs
apt-get clean
rm -rf /var/lib/apt/lists/*
HOME_DIR=/root
curl -o ${HOME_DIR}/oss_compliance.zip https://aws-dlinfra-utilities.s3.amazonaws.com/oss_compliance.zip
unzip ${HOME_DIR}/oss_compliance.zip -d ${HOME_DIR}/
cp ${HOME_DIR}/oss_compliance/test/testOSSCompliance /usr/local/bin/testOSSCompliance
chmod +x /usr/local/bin/testOSSCompliance
chmod +x ${HOME_DIR}/oss_compliance/generate_oss_compliance.sh
${HOME_DIR}/oss_compliance/generate_oss_compliance.sh ${HOME_DIR} ${PYTHON}
rm -rf ${HOME_DIR}/oss_compliance*
EOF

COPY changehostname.c /changehostname.c
