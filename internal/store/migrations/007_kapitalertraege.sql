-- Interest and dividends get their own income category
INSERT INTO categories (grp, slug, name, sort) VALUES
 ('income', 'kapitalertraege', 'Zinsen & Dividenden', 40)
ON CONFLICT (slug) DO NOTHING;
