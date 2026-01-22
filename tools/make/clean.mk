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

##@ Lint Goals

.PHONY: clean
clean:

.PHONY: clean-all
clean-all: clean

clean: clean/coverage
.PHONY: clean/coverage
clean/coverage:
	$(info Clean coverage file...)
	rm -fr coverage.txt

clean: clean/bin
.PHONY: clean/bin
clean/bin:
	$(info Clean artifacts files...)
	rm -fr $(OUTPUT_DIR)

clean: clean/release
.PHONY: clean/release
clean/release:
	$(info Clean release files...)
	rm -fr $(RELEASE_DIR)

clean-all: clean/tools
.PHONY: clean/tools
clean/tools:
	$(info Clean tools folder...)
	[ -d $(TOOLS_BIN)/k8s ] && chmod +w $(TOOLS_BIN)/k8s/* || true
	rm -fr $(TOOLS_BIN)

clean-all: clean/go
.PHONY: clean/go
clean/go:
	$(info Clean golang cache...)
	go clean -cache
