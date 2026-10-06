# Builds the MGvm artifact from this repository (the original mgvm.Dockerfile
# expects the mgvm.tar.gz archive from Zenodo instead).
#
#   docker build -t mgvm .
#   docker run -it mgvm
#   cd scripts && ./1_compile_benchmarks.py && ./2_copy_benchmarks.sh && \
#     ./3_gen_runners.py --preset small && MGVM_JOBS=4 ./4_run_all.sh
FROM golang:1.22

RUN apt-get update && \
    apt-get install -y --no-install-recommends python3 python3-matplotlib vim && \
    rm -rf /var/lib/apt/lists/*

COPY . /root/mgvm
WORKDIR /root/mgvm/mgpusim
RUN go mod download

WORKDIR /root/mgvm/scripts
