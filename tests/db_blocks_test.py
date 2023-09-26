import unittest
import unit_db_test.testcase as dbtest

class CheckIntegrityOfDB(dbtest.DBintegrityTest):
    db_config_file = ".env"

    def test_orphan_blocks_in_block_metrics(self):
        """ Edgy case where two blocks are assigned to the same slot (one of them is an orphan) """
        sql_query = """
        select f_el_block_number, count(*)
        from t_block_metrics
        where f_proposed = true
        group by f_el_block_number
        having count(*) > 1
        """
        df = self.db.get_df_from_sql_query(sql_query)
        self.assertNoRows(df)

    def test_missed_blocks_tagged_as_orphan(self):
        """ Edgy case where a missed blocks is added to the orphan table """
        sql_query = """
        select *
        from t_orphans
        where f_proposed = false
        """
        df = self.db.get_df_from_sql_query(sql_query)
        self.assertNoRows(df)

    def test_transactions_per_block(self):
        """ make sure that the number of tracked transactions match the ones included in the corresponding block """
        sql_query = """
        select t_block_metrics.f_slot, count(distinct(f_hash))
        from t_block_metrics
        inner join t_transactions
        on t_block_metrics.f_slot = t_transactions.f_slot
        group by t_block_metrics.f_slot
        having f_el_transactions != count(distinct(f_hash))
        """
        df = self.db.get_df_from_sql_query(sql_query)
        self.assertNoRows(df)

    def test_block_gaps(self):
        """ make sure that there are no gaps between the indexed blocks """
        sql_query = """
        WITH Gaps AS (
            SELECT
                f_el_block_number AS preceding_block,
                LEAD(f_el_block_number) OVER (ORDER BY f_el_block_number) - 1 AS end_of_missing_range
            FROM
                t_block_metrics
            WHERE f_el_block_number > 16307594
        )
        SELECT
            preceding_block + 1 AS start_of_missing_range,
            end_of_missing_range
        FROM
            Gaps
        WHERE
            end_of_missing_range - preceding_block > 1
        ORDER BY
            preceding_block;        
        """
        df = self.db.get_df_from_sql_query(sql_query)
        self.assertNoRows(df)

    def test_missing_transactions_from_block(self):
        """ Check if there are no blocks that is not present in the transaction table, but had transactions and was proposed """
        sql_query = """
        select *
        from t_block_metrics
        where f_slot not in (
            select distinct(f_slot)
            from t_transactions
        ) and f_el_transactions > 0 and f_proposed = true
        order by f_slot desc
        """
        df = self.db.get_df_from_sql_query(sql_query)
        self.assertNoRows(df)

if __name__ == '__main__':
    unittest.main()

