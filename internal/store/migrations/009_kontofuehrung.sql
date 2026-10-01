-- Account fees and closings get their own category among the fixed costs
INSERT INTO categories (grp, slug, name, sort) VALUES
 ('bills', 'kontofuehrung', 'Kontoführung & Bankgebühren', 130)
ON CONFLICT (slug) DO NOTHING;
