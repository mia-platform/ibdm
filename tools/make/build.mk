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

##@ Go Builds Goals

BUILD_DATE:= $(shell date -u "+%Y-%m-%d")
GO_LDFLAGS+= -s -w
ENABLE_CGO_BUILD?=0

ifdef VERSION_MODULE_NAME
GO_LDFLAGS+= -X $(VERSION_MODULE_NAME).Version=$(VERSION)
GO_LDFLAGS+= -X $(VERSION_MODULE_NAME).BuildDate=$(BUILD_DATE)
endif

SOURCE=$(shell find . -iname "*.go")

$(BUILD_DIR)/%: $(SOURCE)
	$(eval OS:= $(word 1,$(subst /, ,$(*D))))
	$(eval ARCH:= $(word 2,$(subst /, ,$(*D))))
	$(eval ARM:= $(word 3,$(subst /, ,$(*D))))
	$(info Building binary for $(OS) $(ARCH) $(ARM))

	mkdir -p $(@D)
	GOOS=$(OS) GOARCH=$(ARCH) GOARM=$(ARM) CGO_ENABLED=$(ENABLE_CGO_BUILD) go build -trimpath \
		-ldflags "$(GO_LDFLAGS)" -o $@ $(BUILD_PATH)

.PHONY: build
build: $(BUILD_DIR)/$(GOOS)/$(GOARCH)/$(if $(GOARM),/v$(GOARM)/,)$(CMD_NAME)$(if $(filter windows,$(GOOS)),.exe,)
