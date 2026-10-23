-- 前台搜索从 LIKE '%kw%' 改为 FULLTEXT 布尔短语检索。
--
-- 0001 建过同名索引，但只覆盖 (title, summary) 且是默认解析器：
--   - 搜不到正文；默认解析器对中文按空格/标点整词切分，连续汉字整句成
--     一个词，中文关键字基本命不中。
-- 这里删掉重建为 (title, summary, content) + ngram。ngram 解析器按 2 字
-- 滑窗建倒排，对中文等无空格语言等效于子串匹配，MySQL 8.0 内置，无需装插件。
ALTER TABLE posts DROP INDEX ft_posts_search;
ALTER TABLE posts ADD FULLTEXT INDEX ft_posts_search (title, summary, content) WITH PARSER ngram;
