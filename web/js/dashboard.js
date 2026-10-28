/**
 * Дашборд: приветствие, кольцо прогресса и 4 счётчика из GET /api/stats.
 */
import { get } from './state.js';

let root = null;

export function init(container) {
  root = container;
}

export function render() {
  if (!root) return;

  const stats = get('stats');
  const user = get('user');
  const loading = get('loading');

  const heroTitle = document.getElementById('heroTitle');
  const heroText = document.getElementById('heroText');
  const ringValue = document.getElementById('ringValue');

  if (heroTitle) {
    heroTitle.textContent = user ? `С возвращением, ${user.name}!` : 'С возвращением!';
  }

  if (heroText) {
    heroText.textContent = stats
      ? `Активных задач: ${stats.active}. В работе: ${stats.in_progress}. Горящих за 24 ч: ${stats.due_24h}.`
      : 'Загружаем ваши задачи…';
  }

  if (ringValue) ringValue.textContent = stats ? stats.done_7d : '0';

  if (!stats) {
    root.innerHTML = loading.tasks ? '<div class="skeleton"></div>'.repeat(4) : '';
    return;
  }

  const cards = [
    [stats.active, 'Активных задач', ''],
    [stats.in_progress, 'В работе', ''],
    [stats.done_7d, 'Готово за 7 дней', ''],
    [stats.overdue, 'Просрочено', ' is-danger'],
  ];

  root.innerHTML = cards.map(([value, label, tone]) => `
    <div class="stat${tone}">
      <strong>${value}</strong>
      <span>${label}</span>
    </div>
  `).join('');
}
