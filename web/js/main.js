// Homestead web UI. Plain ES modules, no build step and no dependencies.
//
// core/   shared building blocks: constants, formatting, API, state, actions, shell
// views/  one module per page; each registers its view and its actions on import
import { initActions } from './core/actions.js';
import { initShell, route } from './core/shell.js';
import { initTheme } from './core/theme.js';

import './views/budget.js';
import './views/transactions.js';
import './views/recurring.js';
import './views/couple.js';
import './views/accounts.js';
import './views/hv/overview.js';
import './views/hv/leases.js';
import './views/hv/properties.js';
import './views/hv/nk.js';
import './views/hv/rentAccount.js';
import './views/hv/deadlines.js';

initActions();
initTheme();
initShell();
route();
