
		if KEYS[1] == nil then
			return error("spcode empty")
		end

		local spkey = "sp."..KEYS[1]

		local stampkey = "stamp."..KEYS[2]
		local stamp = redis.call("GET","stamp."..KEYS[2])

		 if stamp == KEYS[2] then 
		 	return "data has saved ok"
		 end

		local v = redis.call("GET",KEYS[2])
		


		if v==nil then 
			return error("invalid keys[2]")
		end
			
		local rdecode = cjson.decode(v)
		
		if rdecode["spcode"] ~=KEYS[1] then
			return error("spcode not eq to cmd")
		end


		--redis.call("HMSET","sp."..rdecode["spcode"],"spcode",rdecode["spcode"]
		--					,"spname",rdecode["spname"] ~= nil and rdecode["spname"] or ""
		--					,"validate",rdecode["validate"] ~= nil and rdecode["validate"] or ""
		--					,"credit",rdecode["credit"] ~= nil and rdecode["credit"] or ""
		--					,"maxsend",rdecode["maxsend"] ~= nil and rdecode["maxsend"] or ""
		--					,"maxdaysend",rdecode["maxdaysend"] ~= nil and rdecode["maxdaysend"] or ""
		--					,"maxweeksend",rdecode["maxweeksend"] ~= nil and
		--					,"maxmonsend",rdecode["maxmonsend"] ~= nil and
		--					)
		
		if rdecode.spname ~= nil then
				redis.call("HSET",spkey,"spname",rdecode.spname)
		end

		if rdecode.validate ~= nil then
					redis.call("HSET",spkey,"validate",rdecode.validate)
			end
		if rdecode.credit ~= nil then
					redis.call("HSET",spkey,"credit",rdecode.credit)
			end
		if rdecode.maxsend ~= nil then
					redis.call("HSET",spkey,"maxsend",rdecode.maxsend)
			end
		if rdecode.maxdaysend ~= nil then
					redis.call("HSET",spkey,"maxdaysend",rdecode.maxdaysend)
			end
		if rdecode.maxweeksend ~= nil then
					redis.call("HSET",spkey,"maxweeksend",rdecode.maxweeksend)
			end
		if rdecode.maxmonsend ~= nil then
					redis.call("HSET",spkey,"maxmonsend",rdecode.maxmonsend)
			end
		if rdecode.maxyearsend ~= nil then
					redis.call("HSET",spkey,"maxyearsend",rdecode.maxyearsend)
			end
		if rdecode.userdaymax ~=nil then
					redis.call("HSET",spkey,"userdaymax",rdecode.userdaymax)
			end	
			
		if rdecode.spcode ~= nil then
					redis.call("HSET",spkey,"spcode",rdecode.spcode)
		end

		--标记命令时间戳
		redis.call("SET",stampkey,KEYS[2])
		redis.call("EXPIRE",stampkey,60)
		
		if rdecode.accessnumbers ~= nil then
			local total_str = ""
			for  i = 1, #rdecode.accessnumbers do 
				 total_str = total_str..rdecode.accessnumbers[i].."|"
			end
			redis.call("HSET",spkey,"accessnumbers",total_str)
		end
		
		redis.call("SADD","SPS",rdecode.spcode)
				
		return "save data ok"	
