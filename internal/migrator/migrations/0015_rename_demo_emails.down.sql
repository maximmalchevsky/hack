-- Откат email к старым значениям (инверсия up).
UPDATE users SET email = 'anna@worktime.local'   WHERE email = 'malchevsky@iqj.app';
UPDATE users SET email = 'maria@worktime.local'  WHERE email = 'zharov@iqj.app';
UPDATE users SET email = 'lena@worktime.local'   WHERE email = 'postnikov@iqj.app';
UPDATE users SET email = 'sergey@worktime.local' WHERE email = 'plamadil@worktime.local';
UPDATE users SET email = 'olga@worktime.local'   WHERE email = 'petrov@worktime.local';
UPDATE users SET email = 'dmitry@worktime.local' WHERE email = 'daniil@iqj.app';
UPDATE users SET email = 'tanya@worktime.local'  WHERE email = 'yermolina@iqj.app';
