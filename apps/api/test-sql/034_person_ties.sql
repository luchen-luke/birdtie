BEGIN;
DO $$
DECLARE a uuid; b uuid; request_id uuid; old_request uuid; city text;
BEGIN
  INSERT INTO accounts(id,account_type) VALUES(gen_random_uuid(),'person') RETURNING id INTO a;
  INSERT INTO accounts(id,account_type) VALUES(gen_random_uuid(),'person') RETURNING id INTO b;
  SELECT id INTO city FROM cities ORDER BY id LIMIT 1;
  IF city IS NULL THEN RAISE EXCEPTION '034 fixture requires a city'; END IF;
  INSERT INTO connection_requests(sender_account_id,recipient_account_id,city_id,note,scope,state,expires_at)
    VALUES(a,b,city,'旧版对话','conversation','accepted',now()+interval '1 day') RETURNING id INTO old_request;
  INSERT INTO conversations(request_id,member_a_account_id,member_b_account_id) VALUES(old_request,a,b);
  IF EXISTS(SELECT 1 FROM person_ties WHERE person_a_account_id IN(a,b) OR person_b_account_id IN(a,b)) THEN
    RAISE EXCEPTION 'accepted conversation became friend';
  END IF;
  BEGIN
    INSERT INTO person_ties(person_a_account_id,person_b_account_id,request_id)
      VALUES(LEAST(a,b),GREATEST(a,b),old_request);
    RAISE EXCEPTION 'conversation accepted as Tie';
  EXCEPTION WHEN raise_exception THEN
    IF SQLERRM='conversation accepted as Tie' THEN RAISE; END IF;
  END;
  INSERT INTO connection_requests(sender_account_id,recipient_account_id,city_id,note,scope,state,expires_at)
    VALUES(a,b,NULL,'申请好友','friend','accepted',now()+interval '1 day') RETURNING id INTO request_id;
  INSERT INTO person_ties(person_a_account_id,person_b_account_id,request_id)
    VALUES(LEAST(a,b),GREATEST(a,b),request_id);
  BEGIN
    INSERT INTO person_ties(person_a_account_id,person_b_account_id,request_id)
      VALUES(GREATEST(a,b),LEAST(a,b),request_id);
    RAISE EXCEPTION 'unsorted Tie accepted';
  EXCEPTION WHEN raise_exception OR check_violation THEN
    IF SQLERRM='unsorted Tie accepted' THEN RAISE; END IF;
  END;
  IF (SELECT count(*) FROM person_ties WHERE person_a_account_id IN(a,b) AND person_b_account_id IN(a,b))<>1 THEN
    RAISE EXCEPTION 'Tie not unique';
  END IF;
  BEGIN
    INSERT INTO connection_requests(sender_account_id,recipient_account_id,city_id,note,scope,expires_at)
      VALUES(a,b,city,'好友不能绑定城市','friend',now()+interval '1 day');
    RAISE EXCEPTION 'city-bound friend accepted';
  EXCEPTION WHEN check_violation THEN NULL;
  END;
END $$;
ROLLBACK;
