-- Приводим email демо-пользователей к новым адресам (логин = email).
-- Игорь (igor@) и админ (admin@) не трогаем.
--
-- Защита от UNIQUE-конфликта: если целевой email уже занят (например, его
-- вручную поставили через UI смены почты), UPDATE пропускает строку, а не
-- падает с 23505 — иначе golang-migrate помечает версию dirty.
-- Идемпотентно: повторный прогон ничего не ломает.

UPDATE users SET email = 'maxim@iqj.app'
WHERE email = 'anna@worktime.local'
  AND NOT EXISTS (SELECT 1 FROM users u2 WHERE u2.email = 'maxim@iqj.app');

UPDATE users SET email = 'zharov@iqj.app'
WHERE email = 'maria@worktime.local'
  AND NOT EXISTS (SELECT 1 FROM users u2 WHERE u2.email = 'zharov@iqj.app');

UPDATE users SET email = 'postnikov@iqj.app'
WHERE email = 'lena@worktime.local'
  AND NOT EXISTS (SELECT 1 FROM users u2 WHERE u2.email = 'postnikov@iqj.app');

UPDATE users SET email = 'plamadil@worktime.local'
WHERE email = 'sergey@worktime.local'
  AND NOT EXISTS (SELECT 1 FROM users u2 WHERE u2.email = 'plamadil@worktime.local');

UPDATE users SET email = 'petrov@worktime.local'
WHERE email = 'olga@worktime.local'
  AND NOT EXISTS (SELECT 1 FROM users u2 WHERE u2.email = 'petrov@worktime.local');

UPDATE users SET email = 'daniil@iqj.app'
WHERE email = 'dmitry@worktime.local'
  AND NOT EXISTS (SELECT 1 FROM users u2 WHERE u2.email = 'daniil@iqj.app');

UPDATE users SET email = 'yermolina@iqj.app'
WHERE email = 'tanya@worktime.local'
  AND NOT EXISTS (SELECT 1 FROM users u2 WHERE u2.email = 'yermolina@iqj.app');
