# Copyright Mia srl
# SPDX-License-Identifier: Apache-2.0

# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at

#    http://www.apache.org/licenses/LICENSE-2.0

# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

##@ Release Goals

BUILD_TARGETS := $(RELEASE_DIR)/$(CMD_NAME)-Linux-x86_64 \
	$(RELEASE_DIR)/$(CMD_NAME)-Linux-aarch64 \
	$(RELEASE_DIR)/$(CMD_NAME)-Darwin-x86_64 \
	$(RELEASE_DIR)/$(CMD_NAME)-Darwin-arm64

TARGETS := $(BUILD_TARGETS:%=%.sigstore.json) $(BUILD_TARGETS:%=%.sbom.json)

# if not already installed in the system install a pinned version in tools folder
SYFT_PATH:= $(shell command -v syft 2> /dev/null)
ifndef SYFT_PATH
	SYFT_PATH:=$(TOOLS_BIN)/syft
endif

COSIGN_PATH:= $(shell command -v cosign 2> /dev/null)
ifndef COSIGN_PATH
	COSIGN_PATH:=$(TOOLS_BIN)/cosign
endif

.PHONY: ci-prepare-release
ci-prepare-release: clean/release $(TARGETS)
	$(MAKE) $(RELEASE_DIR)/checksums.txt.sigstore.json

$(BUILD_TARGETS): $(RELEASE_DIR)/% :
	mkdir -p $(@D)
	$(eval PARTS:= $(subst -, ,$*))
	$(eval CMD:= $(firstword $(PARTS)))
	$(eval OS:= $(shell tr '[:upper:]' '[:lower:]' <<< $(word 2,$(PARTS))))
	$(eval ARCH:= $(firstword $(subst ., ,$(lastword $(PARTS)))))
	$(eval ARCH:= $(subst aarch64,arm64,$(subst x86_64,amd64,$(ARCH))))
	$(MAKE) $(BUILD_OUTPUT)/$(OS)/$(ARCH)/$(CMD)
	$(info Copying binary for $* to release dir...)
	cp -r $(BUILD_OUTPUT)/$(OS)/$(ARCH)/$(CMD) $@

$(RELEASE_DIR)/checksums.txt: $(RELEASE_DIR)/$(CMD_NAME)*
	$(info Generating checksums for release binaries...)
	cd $(@D) && shasum -a 256 * > $(@F)

$(RELEASE_DIR)/%.sbom.json: $(RELEASE_DIR)/% $(SYFT_PATH)
	$(info Generating SBOM for $*...)
	$(SYFT_PATH) scan file:$< -o spdx-json=$@

$(RELEASE_DIR)/%.sigstore.json: $(RELEASE_DIR)/% $(COSIGN_PATH)
	$(info Signing $* with cosign...)
ifdef COSIGN_PRIVATE_KEY
 	$(COSIGN_PATH) sign-blob $< --key $(COSIGN_PRIVATE_KEY) --bundle $@ --yes
else
	$(COSIGN_PATH) sign-blob $< --key cosign.key --tlog-upload=false --use-signing-config=false --bundle $@ --yes
endif

$(TOOLS_BIN)/cosign: $(TOOLS_DIR)/COSIGN_VERSION
	$(eval COSIGN_VERSION:= $(shell cat $<))
	mkdir -p $(TOOLS_BIN)
	$(info Installing cosign $(COSIGN_VERSION) bin in $(TOOLS_BIN))
	GOBIN=$(TOOLS_BIN) go install github.com/sigstore/cosign/v3/cmd/cosign@$(COSIGN_VERSION)

$(TOOLS_BIN)/syft: $(TOOLS_DIR)/SYFT_VERSION
	$(eval SYFT_VERSION:= $(shell cat $<))
	mkdir -p $(TOOLS_BIN)
	$(info Installing syft $(SYFT_VERSION) bin in $(TOOLS_BIN))
	GOBIN=$(TOOLS_BIN) go install github.com/anchore/syft/cmd/syft@$(SYFT_VERSION)
