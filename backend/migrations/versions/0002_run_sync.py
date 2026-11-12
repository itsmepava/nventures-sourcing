"""Track immediate per-run sheet sync and verified writes."""
from alembic import op
import sqlalchemy as sa
revision = '0002'
down_revision = '0001'
branch_labels = None
depends_on = None

def upgrade():
    for name in ('sync_to_sheets', 'synced', 'sync_errors'):
        op.add_column('runs', sa.Column(name, sa.Integer(), nullable=False, server_default='0'))

def downgrade():
    with op.batch_alter_table('runs') as batch:
        for name in ('sync_errors', 'synced', 'sync_to_sheets'):
            batch.drop_column(name)
