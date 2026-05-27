-- Откат переименований к старым демо-именам.
UPDATE users SET full_name = 'Анна Соколова'    WHERE email = 'anna@worktime.local';
UPDATE users SET full_name = 'Мария Петрова'    WHERE email = 'maria@worktime.local';
UPDATE users SET full_name = 'Лена Орлова'      WHERE email = 'lena@worktime.local';
UPDATE users SET full_name = 'Сергей Васильев'  WHERE email = 'sergey@worktime.local';
UPDATE users SET full_name = 'Ольга Кузнецова'  WHERE email = 'olga@worktime.local';
UPDATE users SET full_name = 'Дмитрий Соловьёв' WHERE email = 'dmitry@worktime.local';
UPDATE users SET full_name = 'Татьяна Белова'   WHERE email = 'tanya@worktime.local';

UPDATE users SET full_name = 'Алексей Админов'
WHERE role = 'admin' AND full_name = 'Александр Черемисов';
