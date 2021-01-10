# ------------------------------------------------------------------------------
# Copyright 2012-2020 Aerospike, Inc.
#
# Portions may be licensed to Aerospike, Inc. under one or more contributor
# license agreements.
#
# Licensed under the Apache License, Version 2.0 (the "License"); you may not
# use this file except in compliance with the License. You may obtain a copy of
# the License at http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS, WITHOUT
# WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the
# License for the specific language governing permissions and limitations under
# the License.
# ------------------------------------------------------------------------------

#
# Aerospike Kubernetes' Init Container Image
#

FROM golang:buster AS builder

COPY ./peers.go /builder/peers.go
COPY ./main.go /builder/main.go
COPY ./utils.go /builder/utils.go
COPY ./init.go /builder/init.go
COPY ./config.go /builder/config.go

WORKDIR /builder

RUN go get -d ./... \
	&& go build -o init . \
	&& cp init /init

COPY ./aerospike-utility/aku-adm.go /utility/aku-adm.go
COPY ./aerospike-utility/aku-utils.go /utility/aku-utils.go

WORKDIR /utility

RUN go get -d ./... \
	&& go build -o aku-adm aku-adm.go aku-utils.go \
	&& cp aku-adm /aku-adm

FROM debian:buster-slim

COPY --from=builder /init /init
COPY --from=builder /aku-adm /aku-adm

RUN chmod +x /init /aku-adm

ENTRYPOINT [ "/init" ]
CMD ["--log-level", "debug"]
