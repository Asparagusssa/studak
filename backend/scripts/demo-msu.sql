-- Local development fixtures only. Run manually against the local Compose DB.
BEGIN;

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM universities WHERE short_name = 'МГУ') THEN
        RAISE EXCEPTION 'МГУ не найден. Сначала запусти локальный Studak.';
    END IF;
END
$$;

WITH msu AS (
    SELECT id FROM universities WHERE short_name = 'МГУ' ORDER BY id LIMIT 1
),
profiles(web_uuid, name, age, gender, bio) AS (
    VALUES
      ('8af03633-6016-431d-b264-000000000001', 'Аня',    20, 'female', 'Учусь на журфаке, собираю плейлисты и ищу компанию на выставку.'),
      ('8af03633-6016-431d-b264-000000000002', 'Саша',   22, 'female', 'Химфак. После пар бегаю, по выходным пробую новые кофейни.'),
      ('8af03633-6016-431d-b264-000000000003', 'Мила',   19, 'female', 'Люблю настолки и походы. Могу часами обсуждать фильмы.'),
      ('8af03633-6016-431d-b264-000000000004', 'Лера',   23, 'female', 'Мехмат, плёночная фотография и прогулки по Воробьёвым горам.'),
      ('8af03633-6016-431d-b264-000000000005', 'Илья',   21, 'male',   'Физфак. Ищу компанию на квиз и всегда забываю название любимой группы.'),
      ('8af03633-6016-431d-b264-000000000006', 'Тимур',  22, 'male',   'Биофак. Люблю готовить и смотреть документалки про космос.'),
      ('8af03633-6016-431d-b264-000000000007', 'Даня',   20, 'male',   'Играю в баскетбол, иногда пропадаю в библиотеке. Начнём с кофе?'),
      ('8af03633-6016-431d-b264-000000000008', 'Максим', 24, 'male',   'Истфак. Люблю архитектуру, пешие маршруты и спорить о концовках книг.')
)
INSERT INTO users (web_uuid, name, age, gender, bio, my_university_id, search_mode, search_gender)
SELECT p.web_uuid, p.name || ' · тест', p.age, p.gender, 'Тестовая анкета МГУ. ' || p.bio, msu.id, 'all', 'all'
FROM profiles p CROSS JOIN msu
ON CONFLICT (web_uuid) DO NOTHING;

\if :with_likes
DO $$
BEGIN
    IF (SELECT count(*) FROM users WHERE max_id IS NOT NULL) <> 1 THEN
        RAISE EXCEPTION 'Для тестовых лайков нужна ровно одна анкета MAX в локальной базе.';
    END IF;
END
$$;

INSERT INTO likes (from_user_id, to_user_id, is_like, is_mutual)
SELECT fixture.id, tester.id, TRUE, COALESCE(previous.is_like, FALSE)
FROM users AS fixture
CROSS JOIN users AS tester
LEFT JOIN likes AS previous ON previous.from_user_id = tester.id AND previous.to_user_id = fixture.id
WHERE tester.max_id IS NOT NULL
  AND fixture.web_uuid IN ('8af03633-6016-431d-b264-000000000001', '8af03633-6016-431d-b264-000000000005')
ON CONFLICT (from_user_id, to_user_id) DO NOTHING;

-- If the tester previously liked a fixture, keep both match flags consistent.
UPDATE likes AS outgoing
SET is_mutual = true, updated_at = NOW()
FROM users AS fixture, users AS tester, likes AS incoming
WHERE tester.max_id IS NOT NULL
  AND fixture.web_uuid IN ('8af03633-6016-431d-b264-000000000001', '8af03633-6016-431d-b264-000000000005')
  AND outgoing.from_user_id = tester.id AND outgoing.to_user_id = fixture.id
  AND incoming.from_user_id = fixture.id AND incoming.to_user_id = tester.id
  AND outgoing.is_like AND incoming.is_like AND NOT outgoing.is_mutual;
\endif

COMMIT;

SELECT gender, count(*) AS demo_profiles
FROM users WHERE web_uuid LIKE '8af03633-6016-431d-b264-00000000000%'
GROUP BY gender ORDER BY gender;
