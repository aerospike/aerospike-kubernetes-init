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

FROM golang:buster AS peer_finder_builder

COPY ./peer-finder/peer-finder.go /builder/peer-finder.go
WORKDIR /builder
RUN go get -d ./... \
	&& go build -o peer-finder . \
	&& cp peer-finder /peer-finder


FROM debian:buster-slim

COPY install.sh /install.sh
COPY on-start.sh /on-start.sh
COPY --from=peer_finder_builder /peer-finder /peer-finder
COPY aerospike.sh /aerospike.sh

RUN chmod +x /install.sh /on-start.sh /peer-finder /aerospike.sh \
	&& apt update -y \
	&& apt install curl jq -y

ENTRYPOINT [ "/install.sh" ]
