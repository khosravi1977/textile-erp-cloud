INSERT INTO uoms (code, name) VALUES ('KG','Kilogram'),('MTR','Meter'),('ROLL','Roll'),('PCS','Pieces') ON CONFLICT (code) DO NOTHING;

INSERT INTO risk_group_definitions (group_name, min_score, max_score, credit_multiplier, prepayment_percent, allow_check, allow_barter, allow_credit_days)
VALUES ('Low',70,100,1.00,0,true,true,60),('Medium',40,69,0.80,20,true,true,30),('High',0,39,0.50,100,false,false,0) ON CONFLICT DO NOTHING;

INSERT INTO accounts (code, name, type, is_detail) VALUES
('1000','دارایی‌ها','Asset',false),('1100','موجودی نقد','Asset',true),('1200','حساب‌های دریافتنی','Asset',true),
('1300','موجودی مواد و کالا','Asset',true),('1400','دارایی‌های ثابت','Asset',true),('2000','بدهی‌ها','Liability',false),
('2100','حساب‌های پرداختنی','Liability',true),('2200','تسهیلات مالی','Liability',true),('3000','سرمایه','Equity',false),
('3100','سرمایه اولیه','Equity',true),('4000','درآمدها','Income',false),('4100','درآمد کارمزد','Income',true),
('4200','درآمد فروش','Income',true),('5000','هزینه‌ها','Expense',false),('5100','هزینه ضایعات','Expense',true),
('5200','هزینه خواب ماشین','Expense',true),('5300','هزینه مواد اولیه','Expense',true) ON CONFLICT (code) DO NOTHING;

INSERT INTO branches (code, name, created_by) VALUES ('MAIN','شعبه اصلی',1) ON CONFLICT (code) DO NOTHING;
