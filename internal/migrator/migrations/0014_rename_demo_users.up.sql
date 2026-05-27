-- Переименование демо-пользователей: меняем full_name под новые имена.
-- Email (логин) не трогаем — на нём завязана авторизация и тесты.
--
-- Игорь Климов сохраняется — он главный персонаж демо-сценария «Manager».
-- Идемпотентность через WHERE email = '…': UPDATE безопасно повторять.

UPDATE users SET full_name = 'Максим Малчевский'  WHERE email = 'anna@worktime.local';
UPDATE users SET full_name = 'Степан Жаров'        WHERE email = 'maria@worktime.local';
UPDATE users SET full_name = 'Даниил Постников'    WHERE email = 'lena@worktime.local';
UPDATE users SET full_name = 'Олег Пламадил'       WHERE email = 'sergey@worktime.local';
UPDATE users SET full_name = 'Александр Петров'    WHERE email = 'olga@worktime.local';
UPDATE users SET full_name = 'Даниил Игаев'        WHERE email = 'dmitry@worktime.local';
UPDATE users SET full_name = 'Софья Ермолина'      WHERE email = 'tanya@worktime.local';

-- Админа тоже переименуем — пользователь хочет «всех кроме Игоря».
-- Email админа определяется ADMIN_EMAIL — обычно admin@worktime.local.
UPDATE users SET full_name = 'Александр Черемисов'
WHERE role = 'admin' AND full_name = 'Алексей Админов';
