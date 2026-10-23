-- The database the Postgres driver's tests run against (SQLMCP_TEST_PG_DSN).
-- The tests count these rows: 120 users and 400 orders.
create table public.users (
  id         integer primary key,
  email      text not null,
  full_name  text not null,
  country    text not null,
  created_at timestamptz not null default now()
);
insert into public.users (id, email, full_name, country)
select i, 'user' || i || '@example.com', 'User ' || i, (array['DE', 'FR', 'TR', 'US'])[1 + i % 4]
from generate_series(1, 120) i;

create table public.orders (
  id         integer primary key,
  user_id    integer references public.users (id),
  total      numeric(10, 2),
  created_at timestamptz not null default now()
);
insert into public.orders (id, user_id, total)
select i, 1 + i % 120, (i % 97) * 1.25
from generate_series(1, 400) i;

-- A second schema, so the schema listing is seen to span more than public.
create schema billing;
create table billing.cards (
  id          integer primary key,
  user_id     integer references public.users (id),
  card_number text not null
);
insert into billing.cards (id, user_id, card_number)
select i, i, '4000-0000-0000-' || lpad(i::text, 4, '0')
from generate_series(1, 10) i;

-- The least-privileged login the README recommends.
create role reader login password 'reader';
grant usage on schema public to reader;
grant select on public.users, public.orders to reader;
