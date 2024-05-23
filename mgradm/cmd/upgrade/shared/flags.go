// SPDX-FileCopyrightText: 2024 SUSE LLC
//
// SPDX-License-Identifier: Apache-2.0

package shared

import (
	"github.com/spf13/cobra"
	"github.com/uyuni-project/uyuni-tools/mgradm/shared/utils"
	"github.com/uyuni-project/uyuni-tools/shared/types"
)

// UpgradeFlags represents flags used for upgrading a server.
type UpgradeFlags struct {
	Image          types.ImageFlags `mapstructure:",squash"`
	DbUpgradeImage types.ImageFlags `mapstructure:"dbupgrade"`
}

// AddUpgradeFlags add upgrade flags to a command.
func AddUpgradeFlags(cmd *cobra.Command) {
	utils.AddImageUpgradeFlag(cmd)
	utils.AddDbUpgradeImageFlag(cmd)
}

// AddUpgradeListFlags add upgrade list flags to a command.
func AddUpgradeListFlags(cmd *cobra.Command) {
	utils.AddImageUpgradeFlag(cmd)
}
