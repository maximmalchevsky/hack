-- Откат email к старым значениям.
UPDATE users SET email = 'anna@worktime.local'   WHERE email = 'maxim@worktime.local';
UPDATE users SET email = 'maria@worktime.local'  WHERE email = 'stepan@worktime.local';
UPDATE users SET email = 'lena@worktime.local'   WHERE email = 'daniil.p@worktime.local';
UPDATE users SET email = 'sergey@worktime.local' WHERE email = 'oleg@worktime.local';
UPDATE users SET email = 'olga@worktime.local'   WHERE email = 'alexandr@worktime.local';
UPDATE users SET email = 'dmitry@worktime.local' WHERE email = 'daniil.i@worktime.local';
UPDATE users SET email = 'tanya@worktime.local'  WHERE email = 'sofia@worktime.local';
