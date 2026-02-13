// SPDX-License-Identifier: AGPL-3.0-or-later
// DMRHub - Run a DMR network server in a single binary
// Copyright (C) 2023-2026 Jacob McSwain
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program. If not, see <https://www.gnu.org/licenses/>.
//
// The source code is available at <https://github.com/USA-RedDragon/DMRHub>

type ConfirmDialogInput = {
  title?: string;
  description?: string;
};

const toMessage = ({ title, description }: ConfirmDialogInput) => {
  const header = title ? `${title}\n\n` : '';
  return `${header}${description || 'Are you sure?'}`;
};

export function useConfirmDialog() {
  const confirm = ({ title, description }: ConfirmDialogInput): Promise<boolean> => {
    return Promise.resolve(window.confirm(toMessage({ title, description })));
  };

  const alert = ({ title, description }: ConfirmDialogInput): Promise<boolean> => {
    window.alert(toMessage({ title, description }));
    return Promise.resolve(true);
  };

  return {
    confirm,
    alert,
  };
}
