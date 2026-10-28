/**
 * Блок «Аналитика»: рендер rule-based подсказок из GET /api/insights.
 */
import { get } from './state.js';
import { esc, severityIcon } from './ui.js';

let root = null;
let onShowTasks = () => {};

export function init(container, handlers = {}) {
  root = container;
  onShowTasks = handlers.onShowTasks ?? (() => {});

  root.addEventListener('click', (event) => {
    const button = event.target.closest('[data-show]');
    if (!button) return;

    const item = get('insights')[Number(button.dataset.show)];
    if (item?.task_ids?.length) onShowTasks(item.task_ids);
  });
}

export function render() {
  if (!root) return;

  const insights = get('insights');
  const loading = get('loading');

  if (!insights.length) {
    root.innerHTML = loading.insights ? '<div class="skeleton"></div>'.repeat(2) : '';
    return;
  }

  root.innerHTML = insights.map((item, index) => `
    <article class="insight insight-${item.severity}">
      <h3 class="insight-title">
        <span aria-hidden="true">${severityIcon(item.severity)}</span>
        ${esc(item.title)}
      </h3>
      <p>${esc(item.message)}</p>
      ${item.task_ids?.length
        ? `<button class="btn" type="button" data-show="${index}">Показать задачи</button>`
        : ''}
    </article>
  `).join('');
}
