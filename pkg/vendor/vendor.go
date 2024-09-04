/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package vendor

import (
	"os"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/scheme"
)

func InitHostAgentAPIGroup(groupVersion *schema.GroupVersion, schemeBuilder *scheme.Builder) {
	hostAgentAPIGroup := os.Getenv("HOST_CONFIG_AGENT_API_GROUP")
	if hostAgentAPIGroup != "" {
		groupVersion.Group = hostAgentAPIGroup
		schemeBuilder.GroupVersion = *groupVersion
	}
}
