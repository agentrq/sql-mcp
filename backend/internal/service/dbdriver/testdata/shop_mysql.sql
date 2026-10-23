-- The database the MySQL driver's tests run against (SQLMCP_TEST_MYSQL_DSN,
-- SQLMCP_TEST_MARIADB_DSN). Runs on MySQL 8+ and MariaDB 10.6+.
-- The tests count these rows: 120 users and 400 orders.
create database if not exists shop;
use shop;

-- 1..1000, to generate rows from.
create table seq (i int primary key);
insert into seq (i)
select 1 + a.d + 10 * b.d + 100 * c.d
from (select 0 d union all select 1 union all select 2 union all select 3 union all select 4
      union all select 5 union all select 6 union all select 7 union all select 8 union all select 9) a,
     (select 0 d union all select 1 union all select 2 union all select 3 union all select 4
      union all select 5 union all select 6 union all select 7 union all select 8 union all select 9) b,
     (select 0 d union all select 1 union all select 2 union all select 3 union all select 4
      union all select 5 union all select 6 union all select 7 union all select 8 union all select 9) c;

create table users (
  id         int primary key,
  email      varchar(255) not null,
  full_name  varchar(255) not null,
  country    char(2) not null,
  created_at timestamp not null default current_timestamp
);
insert into users (id, email, full_name, country)
select i, concat('user', i, '@example.com'), concat('User ', i), elt(1 + i % 4, 'DE', 'FR', 'TR', 'US')
from seq where i <= 120;

create table orders (
  id         int primary key,
  user_id    int references users (id),
  total      decimal(10, 2),
  created_at timestamp not null default current_timestamp
);
insert into orders (id, user_id, total)
select i, 1 + i % 120, (i % 97) * 1.25
from seq where i <= 400;

drop table seq;

-- A second database, so the schema listing is seen to span more than one.
create database billing;
create table billing.cards (
  id          int primary key,
  user_id     int,
  card_number varchar(32) not null
);
insert into billing.cards (id, user_id, card_number)
select id, id, concat('4000-0000-0000-', lpad(id, 4, '0'))
from shop.users where id <= 10;

-- The least-privileged login the README recommends.
create user 'reader'@'%' identified by 'reader';
grant select on shop.users to 'reader'@'%';
grant select on shop.orders to 'reader'@'%';
