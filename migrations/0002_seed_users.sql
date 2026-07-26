INSERT INTO users (id, name, email, roles) VALUES
  ('00000000-0000-0000-0000-000000000001', '产品 A',      'pm.a@example.com',      '{SPONSOR}'),
  ('00000000-0000-0000-0000-000000000002', '技术 Leader B','lead.b@example.com',    '{SPONSOR,TECH_LEAD}'),
  ('00000000-0000-0000-0000-000000000003', '研发 C',      'eng.c@example.com',     '{ENGINEER}'),
  ('00000000-0000-0000-0000-000000000004', '研发 D',      'eng.d@example.com',     '{ENGINEER}'),
  ('00000000-0000-0000-0000-000000000005', '研发 E',      'eng.e@example.com',     '{ENGINEER}'),
  ('00000000-0000-0000-0000-000000000006', 'Steward F',   'steward.f@example.com', '{STEWARD}')
ON CONFLICT (id) DO NOTHING;
